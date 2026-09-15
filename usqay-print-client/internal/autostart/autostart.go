// Package autostart registers/removes the client so it starts automatically
// with the system, in the current user's session.
//
// On Windows this uses the Task Scheduler (schtasks), not a Windows Service:
// a real service runs in Session 0, isolated from the desktop, and cannot
// show a tray icon or any UI. A logon task runs in the interactive user
// session instead, so the tray stays visible while still starting
// automatically without a manual double-click.
package autostart
