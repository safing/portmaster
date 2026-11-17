import { Injectable } from "@angular/core";
import { Observable, of, throwError } from "rxjs";
import { FeatureID } from "./features";
import { Feature, Pin, SPNStatus, UserProfile } from "./spn.types";

/**
 * portmasterpro ships without the Safing Private Network and without user
 * accounts. This service keeps the public API that the UI components rely on,
 * but never talks to an account server or the (never started) SPN module:
 *
 *  - the profile is a static, local one that unlocks every local feature
 *    (network history, bandwidth visibility); SPN is reported as unavailable,
 *  - the SPN status is permanently "disabled",
 *  - login/logout fail locally without any network request.
 */

/** Static local profile. Every local feature is available, SPN is not. */
export const LOCAL_PROFILE: UserProfile = {
  username: "local",
  state: "local",
  balance: 0,
  device: null,
  subscription: null,
  current_plan: {
    name: "portmasterpro",
    amount: 0,
    months: 0,
    renewable: false,
    feature_ids: [FeatureID.History, FeatureID.Bandwidth, FeatureID.VPNCompat],
  },
  next_plan: null,
  view: null,
};

/** Static SPN status: the SPN module does not exist in portmasterpro. */
export const SPN_DISABLED_STATUS: SPNStatus = {
  Status: "disabled",
  HomeHubID: "",
  HomeHubName: "",
  ConnectedIP: "",
  ConnectedTransport: "",
  ConnectedCountry: null,
  ConnectedSince: null,
};

/** Error returned by every removed account operation. */
export const ERR_ACCOUNT_REMOVED = "Account authentication has been removed from portmasterpro";

@Injectable()
export class SPNService {

  /** Emits the SPN status; permanently disabled. */
  readonly status$: Observable<SPNStatus> = of(SPN_DISABLED_STATUS);

  /** Emits the static local profile. */
  readonly profile$: Observable<UserProfile | null> = of(LOCAL_PROFILE);

  /**
   * Watches all pins of the "main" SPN map. Always empty.
   */
  watchPins(): Observable<Pin[]> {
    return of([] as Pin[]);
  }

  /**
   * Removed: there is no SPN user account. Fails locally, no request is sent.
   */
  login(_: { username: string, password: string }): Observable<never> {
    return throwError(() => new Error(ERR_ACCOUNT_REMOVED));
  }

  /**
   * Removed: there is no SPN user account. Fails locally, no request is sent.
   */
  logout(_purge = false): Observable<never> {
    return throwError(() => new Error(ERR_ACCOUNT_REMOVED));
  }

  /** There are no purchasable feature packages. */
  watchEnabledFeatures(): Observable<(Feature & { enabled: boolean })[]> {
    return of([]);
  }

  /** There are no purchasable feature packages. */
  loadFeaturePackages(): Observable<Feature[]> {
    return of([]);
  }

  /**
   * Returns the static local profile. The refresh flag is ignored; nothing
   * is fetched from a ticket agent.
   */
  userProfile(_refresh = false): Observable<UserProfile> {
    return of(LOCAL_PROFILE);
  }

  /**
   * Emits the static local profile once.
   */
  watchProfile(): Observable<UserProfile | null> {
    return of(LOCAL_PROFILE);
  }
}
