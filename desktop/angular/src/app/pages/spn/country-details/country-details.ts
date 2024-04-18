import { ChangeDetectionStrategy, ChangeDetectorRef, Component, EventEmitter, Inject, Input, OnChanges, OnDestroy, OnInit, Optional, Output, SimpleChanges, TrackByFunction } from "@angular/core";
import { AppProfile, AppProfileService, Netquery } from '@safing/portmaster-api';
import { SFNG_DIALOG_REF, SfngDialogRef, SfngDialogService } from "@safing/ui";
import { Subscription, forkJoin, of, switchMap } from 'rxjs';
import { repeat } from 'rxjs/operators';
import { MapPin, MapService } from './../map.service';
import { PinDetailsComponent } from './../pin-details/pin-details';

@Component({
  templateUrl: './country-details.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [
    `:host{
      display: block;
      min-width: 630px;
      height: 400px;
      overflow: hidden;
    }`
  ]
})
export class CountryDetailsComponent implements OnInit, OnChanges, OnDestroy {
  /** Subscription to poll map pins and profiles. */
  private subscription = Subscription.EMPTY;

  /** The two letter ISO country code */
  @Input()
  countryCode: string = '';

  /** The name of the country */
  @Input()
  countryName: string = '';

  /** Emits the ID of the pin that is hovered in the list. null if no pin is hovered */
  @Output()
  pinHover = new EventEmitter<string | null>();

  /** @private - The list of pins available in this country */
  pins: MapPin[] = [];

  /** @private - A list of app profiles that use this country as an exit node */
  profiles: { profile: AppProfile, count: number }[] = [];

  /** @private - A {@link TrackByFunction} for all profiles that use this country for exit */
  trackProfile: TrackByFunction<this['profiles'][0]> = (_: number, profile: this['profiles'][0]) => `${profile.profile.Source}/${profile.profile.ID}`;

  /** The number of alive nodes in this country */
  totalAliveCount = 0;

  /** The number of exit nodes in this country */
  exitNodeCount = 0;

  /** The number of active (used) nodes in this country */
  activeNodeCount = 0;

  /** The number of active (used) nodes operated by safing */
  activeSafingNodeCount = 0;

  /** The number of active (used) nodes operated by the community */
  activeCommunityNodeCount = 0;

  /** The number of nodes operated by safing */
  safingNodeCount = 0;

  /** The number of exit nodes operated by safing */
  safingExitNodeCount = 0;

  /** The number of nodes operated by a community member */
  communityNodeCount = 0;

  /** The number of exit ndoes operated by the community */
  communityExitNodeCount = 0;

  /** holds the text format of a netquery search to show all connections that exit in this country */
  filterConnectionsByCountryNodes = '';

  constructor(
    private mapService: MapService,
    private netquery: Netquery,
    private appService: AppProfileService,
    private cdr: ChangeDetectorRef,
    private dialog: SfngDialogService,
    @Inject(SFNG_DIALOG_REF) @Optional() public dialogRef?: SfngDialogRef<CountryDetailsComponent, never, { code: string, name: string }>,
  ) { }

  openPinDetails(id: string) {
    this.dialog.create(PinDetailsComponent, {
      data: id,
      backdrop: false,
      autoclose: true,
      dragable: true,
    })
  }

  ngOnInit() {
    // if we got opened as a dialog we get the code and name of the country
    // from the dialogRef.data field.
    if (!!this.dialogRef) {
      this.countryCode = this.dialogRef.data.code;
      this.countryName = this.dialogRef.data.name;
    }

    this.subscription.unsubscribe();

    this.subscription =
      this.mapService
        .pins$
        .pipe(
          switchMap(pins => {
            // get a list of pins in that country
            const countryPins = pins.filter(pin => pin.entity.Country === this.countryCode);

            // prepare a netquery query that loads the IDs of all profiles that use one of the countries
            // pins as an exit node. Then, map those IDs to the actual app profile object
            const profiles = this.netquery
              .query({
                select: [
                  'profile',
                  { $count: { field: '*', as: 'totalCount' } }
                ],
                groupBy: ['profile'],
                query: {
                  'exit_node': {
                    $in: countryPins.map(pin => pin.pin.ID),
                  }
                }
              }, 'get-connections-per-profile-in-country')
              .pipe(
                switchMap(queryResult => {
                  if (queryResult.length === 0) {
                    return of([]);
                  }

                  return forkJoin(
                    queryResult.map(row => forkJoin({
                      profile: this.appService.getAppProfile(row.profile!),
                      count: of(row.totalCount),
                    })
                    )
                  )
                }),
              );

            return forkJoin({
              pins: of(countryPins),
              profiles: profiles,
            })
          }
          ),
          repeat({
            delay: 5000
          }),
        )
        .subscribe(result => {
          this.pins = result.pins;
          this.profiles = result.profiles

          this.activeNodeCount = 0;
          this.activeCommunityNodeCount = 0;
          this.activeSafingNodeCount = 0;
          this.exitNodeCount = 0;
          this.safingNodeCount = 0;
          this.communityNodeCount = 0;
          this.safingExitNodeCount = 0;
          this.communityExitNodeCount = 0;

          this.pins.forEach(pin => {
            if (pin.isOffline) {
              return
            }
            this.totalAliveCount++;

            if (pin.pin.VerifiedOwner === 'Safing') {
              this.safingNodeCount++;

              if (pin.isExit) {
                this.exitNodeCount++;
                this.safingExitNodeCount++;
              }
              if (pin.isActive) {
                this.activeSafingNodeCount++;
                this.activeNodeCount++;
              }

            } else {
              this.communityNodeCount++;

              if (pin.isExit) {
                this.exitNodeCount++;
                this.communityExitNodeCount++;
              }
              if (pin.isActive) {
                this.activeCommunityNodeCount++;
                this.activeNodeCount++;
              }
            }
          })

          // create a netquery text-query in the format of "exit_node:<id1> exit_node:<id2> ..."
          this.filterConnectionsByCountryNodes = this.pins.map(pin => `exit_node:${pin.pin.ID}`).join(" ")

          this.cdr.markForCheck();
        })
  }

  ngOnChanges(changes: SimpleChanges): void {
    // if we are rendered as a regular component (not as a dialog) we need to
    // handle updates to our @Inputs().
    // just let ngOnInit() do it's thing if the countryCode changed.
    if (!!changes['countryCode']) {
      this.ngOnInit();
    }
  }

  ngOnDestroy() {
    this.subscription.unsubscribe();
  }
}
