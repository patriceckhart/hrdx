// Built only on darwin through the _darwin file suffix.

#import <AppKit/AppKit.h>
#import <UserNotifications/UserNotifications.h>
#import "notify_darwin.h"

extern void hrdxNotificationClicked(int paneID);

// The app requires macOS 12 (Info.plist LSMinimumSystemVersion); Wails
// compiles with an older deployment target, so declare the real floor.
API_AVAILABLE(macos(12.0))
@interface HrdxNotifyDelegate : NSObject <UNUserNotificationCenterDelegate>
@end

@implementation HrdxNotifyDelegate
// Show banners even while the app is frontmost; the core only notifies for
// panes the user is not looking at.
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
       willPresentNotification:(UNNotification *)notification
         withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
	completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList);
}

- (void)userNotificationCenter:(UNUserNotificationCenter *)center
didReceiveNotificationResponse:(UNNotificationResponse *)response
         withCompletionHandler:(void (^)(void))completionHandler {
	NSNumber *pane = response.notification.request.content.userInfo[@"pane"];
	if (pane != nil) {
		hrdxNotificationClicked(pane.intValue);
	}
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp activateIgnoringOtherApps:YES];
	});
	completionHandler();
}
@end

static id hrdxDelegate;

// UNUserNotificationCenter throws outside an app bundle (go run, tests).
static int hrdxNotifyAvailable(void) {
	return [[NSBundle mainBundle] bundleIdentifier] != nil;
}

void hrdxNotifySetup(void) {
	if (!hrdxNotifyAvailable()) return;
	if (@available(macOS 12.0, *)) {
		UNUserNotificationCenter *center = [UNUserNotificationCenter currentNotificationCenter];
		hrdxDelegate = [HrdxNotifyDelegate new];
		center.delegate = hrdxDelegate;
		[center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionBadge)
		                      completionHandler:^(BOOL granted, NSError *error) {}];
	}
}

void hrdxNotify(const char *title, const char *body, int paneID) {
	if (!hrdxNotifyAvailable()) return;
	if (@available(macOS 12.0, *)) {
		UNMutableNotificationContent *content = [UNMutableNotificationContent new];
		content.title = [NSString stringWithUTF8String:title];
		content.body = [NSString stringWithUTF8String:body];
		content.userInfo = @{@"pane": @(paneID)};
		// One banner per pane: a newer one replaces the older.
		NSString *ident = [NSString stringWithFormat:@"hrdx-pane-%d", paneID];
		UNNotificationRequest *request = [UNNotificationRequest requestWithIdentifier:ident content:content trigger:nil];
		[[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:request withCompletionHandler:nil];
	}
}

void hrdxBadge(int count) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSApp.dockTile.badgeLabel = count > 0 ? [NSString stringWithFormat:@"%d", count] : nil;
	});
}
