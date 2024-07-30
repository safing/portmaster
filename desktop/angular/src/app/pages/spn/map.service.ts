import { Injectable } from '@angular/core';
import { AppProfile, GeoCoordinates, IntelEntity, Netquery, Pin, SPNService, UnknownLocation, getPinCoords } from '@safing/portmaster-api';
import { BehaviorSubject, Observable, combineLatest, debounceTime, interval, of, startWith, switchMap } from 'rxjs';
import { distinctUntilChanged, filter, map, share } from 'rxjs/operators';
import { SPNStatus } from './../../../../projects/safing/portmaster-api/src/lib/spn.types';

export interface MapPin {
  pin: Pin;
  // location is set to the geo-coordinates that should be used
  // for that pin.
  location: GeoCoordinates;
  // entity is set to the intel entity that should be used for
  // this pin.
  entity: IntelEntity;

  // whether the pin is regarded as offline / not available.
  isOffline: boolean;

  // whether or not the pin is currently used as an exit node
  isExit: boolean;

  // whether or not the pin is used as a transit node
  isTransit: boolean;

  // whether or not the pin is currently active.
  isActive: boolean;

  // whether or not the pin is used as the entry-node.
  isHome: boolean;

  // whether the pin has any known issues
  hasIssues: boolean;
}

@Injectable({ providedIn: 'root' })
export class MapService {
  /**
   * activeSince$ emits the pre-formatted duration since the SPN is active
   * it formats the duration as "HH:MM:SS" or null if the SPN is not enabled.
   */
  activeSince$: Observable<string | null>;

  /** Emits the current status of the SPN */
  status$: Observable<SPNStatus['Status']>;

  /** Emits all map pins */
  _pins$ = new BehaviorSubject<MapPin[]>([]);

  get pins$(): Observable<MapPin[]> {
    return this._pins$.asObservable();
  }

  pinsMap$ = this.pins$
    .pipe(
      filter(allPins => !!allPins.length),
      map(allPins => {
        const lm = new Map<string, MapPin>();
        allPins.forEach(pin => lm.set(pin.pin.ID, pin));

        return lm
      }),
      share(),
    )

  constructor(
    private spnService: SPNService,
    private netquery: Netquery,
  ) {
    this.status$ = this.spnService
      .status$
      .pipe(
        map(status => !!status ? status.Status : 'disabled'),
        distinctUntilChanged()
      );

    // setup the activeSince$ observable that emits every second how long the
    // SPN has been active.
    this.activeSince$ = combineLatest([
      this.spnService.status$,
      interval(1000).pipe(startWith(-1))
    ]).pipe(
      map(([status]) => !!status.ConnectedSince ? this.formatActiveSinceDate(status.ConnectedSince) : null),
      share(),
    );

    let pinMap = new Map<string, MapPin>();
    let pinResult: MapPin[] = [];

    // create a stream of pin updates from the SPN service if it is enabled.
    this.status$
      .pipe(
        switchMap(status => {
          if (status !== 'disabled') {
            return combineLatest([
              this.spnService.watchPins(),
              interval(5000)
                .pipe(
                  startWith(-1),
                  switchMap(() => this.getPinIDsUsedAsExit())
                )
            ])
          }
          return of([[], []]);
        }),
        map(([pins, exitPinIDs]) => {
          const exitPins = new Set(exitPinIDs);
          const activePins = new Set<string>();
          const transitPins = new Set<string>();
          const seenPinIDs = new Set<string>();

          let hasChanges = false;

          pins.forEach(pin => pin.Route?.forEach((hop, index) => {
            if (index < pin.Route!.length - 1) {
              transitPins.add(hop)
            }

            activePins.add(hop);
          }));

          pins.forEach(pin => {
            // Save Pin ID as seen.
            seenPinIDs.add(pin.ID);

            const oldPinModel = pinMap.get(pin.ID);

            // Get states of new model.
            const isOffline = pin.States.includes('Offline') || !pin.States.includes('Reachable');
            const isHome = pin.HopDistance === 1;
            const isTransit = transitPins.has(pin.ID);

            const isExit = exitPins.has(pin.ID);
            const isActive = activePins.has(pin.ID);
            const hasIssues = pin.States.includes('ConnectivityIssues');

            const pinHasChanged = !oldPinModel || oldPinModel.pin !== pin ||
              oldPinModel.isOffline !== isOffline || oldPinModel.isHome !== isHome || oldPinModel.isTransit !== isTransit ||
              oldPinModel.isExit !== isExit || oldPinModel.isActive !== isActive || oldPinModel.hasIssues !== hasIssues;

            if (pinHasChanged) {
              const newPinModel: MapPin = {
                pin: pin,
                location: getPinCoords(pin) || UnknownLocation,
                entity: (pin.EntityV4 || pin.EntityV6)!,
                isExit,
                isTransit,
                isActive,
                isOffline,
                isHome,
                hasIssues,
              }

              pinMap.set(pin.ID, newPinModel);

              hasChanges = true;
            }
          })

          for (let key of pinMap.keys()) {
            if (!seenPinIDs.has(key)) {
              // this pin has been removed
              pinMap.delete(key)
              hasChanges = true;
            }
          }

          if (hasChanges) {
            pinResult = Array.from(pinMap.values());
          }

          return pinResult;
        }),
        debounceTime(10),
        distinctUntilChanged(),
      )
      .subscribe(pins => this._pins$.next(pins))
  }

  getExitPinIDsForProfile(profile: AppProfile) {
    return this.netquery
      .query({
        select: ['exit_node'],
        groupBy: ['exit_node'],
        query: {
          profile: { $eq: `${profile.Source}/${profile.ID}` },
        }
      }, 'map-service-get-exit-pin-ids-for-profile')
      .pipe(map(result => result.map(row => row.exit_node!)))
  }

  getPinIDsWithActiveSession() {
    return this.pins$
      .pipe(
        map(result => result.filter(pin => pin.pin.SessionActive).map(pin => pin.pin.ID))
      )
  }

  getPinIDsUsedAsExit() {
    return this.netquery
      .query({
        select: ['exit_node'],
        groupBy: ['exit_node']
      }, 'map-service-get-pins-used-as-exit')
      .pipe(
        map(result => result.map(row => row.exit_node!))
      )
  }

  getPinIDsWithActiveConnections() {
    return this.netquery.query({
      select: ['exit_node'],
      groupBy: ['exit_node'],
      query: {
        active: { $eq: true }
      }
    }, 'map-service-get-pins-with-connections')
      .pipe(
        map(activeExitNodes => {
          const pins = this._pins$.getValue();

          const pinIDs = new Set<string>();
          const pinLookupMap = new Map<string, MapPin>();

          pins.forEach(p => pinLookupMap.set(p.pin.ID, p))

          activeExitNodes.map(row => {
            const pin = pinLookupMap.get(row.exit_node!);
            if (!!pin) {
              pin.pin.Route?.forEach(hop => {
                pinIDs.add(hop)
              })
            }
          })

          return Array.from(pinIDs);
        })
      )
  }

  private formatActiveSinceDate(date: string): string {
    const d = new Date(date);
    const diff = Math.floor((new Date().getTime() - d.getTime()) / 1000);
    const hours = Math.floor(diff / 3600);
    const minutes = Math.floor((diff - (hours * 3600)) / 60);
    const secs = diff - (hours * 3600) - (minutes * 60);
    const pad = (d: number) => d < 10 ? `0${d}` : '' + d;

    return `${pad(hours)}:${pad(minutes)}:${pad(secs)}`;
  }
}
