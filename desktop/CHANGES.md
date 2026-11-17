# Desktop UI changes in portmasterpro

The production UI is the Angular app in `desktop/angular` (Angular 16, npm,
Karma). All account, subscription and SPN gating is handled centrally in the
`@safing/portmaster-api` library so that the ~90 components which read the
user profile did not have to be touched individually.

## Central change: `projects/safing/portmaster-api/src/lib/spn.service.ts`

| Member | Before | Now |
|---|---|---|
| `profile$` / `watchProfile()` / `userProfile()` | watched `core:spn/account/user`, fetched `/v1/spn/account/user/profile` | static `LOCAL_PROFILE` with `feature_ids = [history, bw-vis, vpn-compat]`; SPN not included |
| `status$` | watched `runtime:spn/status` | static `disabled` status |
| `watchPins()` | watched `map:main/` | always `[]` |
| `login()` / `logout()` | HTTP to `/v1/spn/account/...` | fail locally with `ERR_ACCOUNT_REMOVED`, no request |
| `loadFeaturePackages()` / `watchEnabledFeatures()` | HTTP to `/v1/account/features` | always `[]` |

Effect: every `profile?.current_plan?.feature_ids?.includes(FeatureID.History)`
or `FeatureID.Bandwidth` check in the app (monitor, app-view, netquery,
dashboard, generic-setting upgrade lock) evaluates to `true`; every
`FeatureID.SPN` check evaluates to `false`. No outbound request to
`account.safing.io` or the ticket agent is issued by the UI.

## Removed from the UI

- Navigation: SPN map entry, "Re-Initialize SPN", "Logout Completely".
- Route `/spn` (the page component still compiles but is unreachable;
  `**` redirects to the dashboard).
- Dashboard: "Features" (package upsell) widget, "Login / Subscribe" /
  "Account Details" header, current-plan/billing text, "SPN Identities"
  mini-stat, "Connections Tunneled through SPN" chart.
- Upsell copy: "Available in Portmaster Plus/Pro" and the pricing link in the
  monitor page.

## Left in place (dead but harmless)

`spn-login`, `spn-account-details`, `spn-status`, `feature-scout`, `pages/spn`
still exist as components. They are either never rendered (`spnLoginRequired`
is `false`, SPN status is `disabled`, developer-expertise gated) or no longer
routed. Removing them entirely is optional clean-up.

## Verify

```bash
cd desktop/angular
npm ci
npm run build
grep -rn 'account.safing.io\|/v1/spn/account\|/v1/account/features' projects/safing/portmaster-api/src
# expected: no matches in spn.service.ts
```
