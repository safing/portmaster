import { ChangeDetectionStrategy, ChangeDetectorRef, Component, OnDestroy, OnInit } from "@angular/core";
import { Subscription } from 'rxjs';
import { MapService } from './../map.service';

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: 'spn-map-legend',
  templateUrl: './map-legend.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SpnMapLegendComponent implements OnInit, OnDestroy {
  private subscription = Subscription.EMPTY;

  safingNodeCount = 0;
  safingExitCount = 0;
  safingActiveCount = 0;

  communityNodeCount = 0;
  communityExitCount = 0;
  communityActiveCount = 0;

  constructor(
    private mapService: MapService,
    private cdr: ChangeDetectorRef,
  ) { }

  ngOnInit() {
    this.subscription = this.mapService
      .pins$
      .subscribe(pins => {
        this.safingActiveCount = 0;
        this.safingExitCount = 0;
        this.safingNodeCount = 0;
        this.communityActiveCount = 0;
        this.communityExitCount = 0;
        this.communityNodeCount = 0;

        pins.forEach(pin => {
          if (pin.pin.VerifiedOwner === 'Safing') {
            if (pin.isActive) {
              this.safingActiveCount++;
            }

            if (pin.isExit) {
              this.safingExitCount++
            }

            this.safingNodeCount++
          } else {
            if (pin.isActive) {
              this.communityActiveCount++;
            }

            if (pin.isExit) {
              this.communityExitCount++;
            }

            this.communityNodeCount++;
          }
        })

        this.cdr.markForCheck();
      })
  }

  ngOnDestroy() {
    this.subscription.unsubscribe();
  }
}
