import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Inject, Input, OnChanges, OnDestroy, OnInit, Optional, SimpleChanges } from '@angular/core';
import { Netquery } from '@safing/portmaster-api';
import { SFNG_DIALOG_REF, SfngDialogRef } from '@safing/ui';
import { Subscription, forkJoin, map, of, switchMap } from 'rxjs';
import { LaneModel } from '../pin-list/pin-list';
import { MapPin, MapService } from './../map.service';

@Component({
  templateUrl: './pin-details.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PinDetailsComponent implements OnInit, OnChanges, OnDestroy {
  private subscription = Subscription.EMPTY;

  @Input()
  mapPinID!: string;

  pin: MapPin | null = null;

  /** Holds all pins this pin has a active connection to */
  connectedPins: LaneModel[] = [];

  /** The number of connections that exit at this pin */
  exitConnectionCount: number = 0;

  constructor(
    private mapService: MapService,
    private netquery: Netquery,
    private cdr: ChangeDetectorRef,
    @Optional() @Inject(SFNG_DIALOG_REF) public dialogRef?: SfngDialogRef<PinDetailsComponent, never, string>,
  ) { }

  ngOnInit(): void {
    // if we got opened via a dialog we get the map pin ID from the dialog data.
    if (!!this.dialogRef) {
      this.mapPinID = this.dialogRef.data;
    }

    this.subscription.unsubscribe();

    this.subscription = this.mapService
      .pins$
      .pipe(
        map(pins => {
          return [pins.find(p => p.pin.ID === this.mapPinID), pins] as [MapPin, MapPin[]];
        }),
        switchMap(([pin, allPins]) => forkJoin({
          pin: of(pin),
          allPins: of(allPins),
          exitConnections: this.netquery.query({
            select: [
              { $count: { field: '*', as: 'totalCount', } },
            ],
            query: {
              exit_node: pin.pin.ID,
            },
            groupBy: ['exit_node']
          }, 'pin-details-get-connections-per-exit-node')
        }))
      )
      .subscribe((result) => {
        this.pin = result.pin || null;

        const lm = new Map<string, MapPin>();
        result.allPins.forEach(pin => lm.set(pin.pin.ID, pin))

        const connectedTo = this.pin?.pin.ConnectedTo || {};
        this.connectedPins = Object.keys(connectedTo)
          .map(pinID => {
            const pin = lm.get(pinID)!;
            return {
              ...connectedTo[pinID],
              mapPin: pin,
            }
          });

        if (result.exitConnections.length) {
          // we expect only one row to be returned for the above query.
          this.exitConnectionCount = result.exitConnections[0].totalCount;
        } else {
          this.exitConnectionCount = 0;
        }

        this.cdr.markForCheck();
      })
  }

  ngOnChanges(changes: SimpleChanges) {
    // if we got rendered directly (without a dialog) we need to
    // handle updates to the mapPinID input field by re-loading the
    // pin details. We do that by simply re-running ngOnInit
    if (!!changes['mapPinID']) {
      this.ngOnInit()
    }
  }

  ngOnDestroy(): void {
    this.subscription.unsubscribe();
  }
}
