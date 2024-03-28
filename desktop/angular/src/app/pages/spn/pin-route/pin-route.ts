import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Input } from "@angular/core";
import { TunnelNode } from "@safing/portmaster-api";
import { take } from 'rxjs';
import { MapPin, MapService } from './../map.service';

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: 'sfng-spn-pin-route',
  templateUrl: './pin-route.html',
  styleUrls: ['./pin-route.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class SpnPinRouteComponent {
  @Input()
  set route(path: (string | MapPin | TunnelNode)[] | null) {
    this.mapService
      .pinsMap$
      .pipe(
        take(1),
      )
      .subscribe(lm => {
        this._route = (path || []).map(idOrPin => {
          if (typeof idOrPin === 'string') {
            return lm.get(idOrPin)!;
          }

          if ('ID' in idOrPin) { // TunnelNode
            return lm.get(idOrPin.ID)!
          }

          return idOrPin;
        });

        this.cdr.markForCheck();
      })
  }
  get route(): MapPin[] {
    return this._route
  }
  private _route: MapPin[] = [];

  constructor(
    private mapService: MapService,
    private cdr: ChangeDetectorRef,
  ) { }
}
