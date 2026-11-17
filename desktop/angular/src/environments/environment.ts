// This file can be replaced during build by using the `fileReplacements` array.
// `ng build --prod` replaces `environment.ts` with `environment.prod.ts`.
// The list of file replacements can be found in `angular.json`.

export const environment = {
  production: false,
  portAPI: "ws://127.0.0.1:817/api/database/v1",
  httpAPI: "http://127.0.0.1:817/api",
  // Issues are opened in the browser on the project repository; nothing is sent to Safing.
  issueTracker: "https://github.com/0n113/portmasterpro/issues"
};

/*
 * For easier debugging in development mode, you can import the following file
 * to ignore zone related error stack frames such as `zone.run`, `zoneDelegate.invokeTask`.
 *
 * This import should be commented out in production mode because it will have a negative impact
 * on performance if an error is thrown.
 */
import 'zone.js/dist/zone-error';  // Included with Angular CLI.
