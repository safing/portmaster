import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, EventEmitter, Input, Output, TrackByFunction } from '@angular/core';
import { Lane } from '@safing/portmaster-api';
import { take } from 'rxjs/operators';
import { MapPin } from '../map.service';
import { MapService } from './../map.service';

export interface LaneModel extends Lane {
  mapPin: MapPin;
}

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: 'spn-pin-list',
  templateUrl: './pin-list.html',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class SpnPinListComponent {
  @Input()
  set allowHover(v: any) {
    this._allowHover = coerceBooleanProperty(v);
  }
  get allowHover() { return this._allowHover }
  private _allowHover = true;

  @Input()
  set allowClick(v: any) {
    this._allowClick = coerceBooleanProperty(v);
  }
  get allowClick() { return this._allowClick }
  private _allowClick = true;

  @Input()
  set pins(pins: (string | MapPin | LaneModel)[]) {
    this.mapService
      .pinsMap$
      .pipe(take(1))
      .subscribe(allPins => {
        this.lanes = null;

        this._pins = (pins || []).map(idOrPin => {
          if (typeof idOrPin === 'string') {
            return allPins.get(idOrPin)!;
          }

          if ('mapPin' in idOrPin) { // LaneModel
            if (this.lanes === null) {
              this.lanes = new Map();
            }

            this.lanes.set(idOrPin.HubID, {
              Capacity: idOrPin.Capacity,
              Latency: idOrPin.Latency,
            })

            return idOrPin.mapPin;
          }

          return idOrPin; // MapPin
        })

        this.cdr.markForCheck();
      })
  }
  get pins(): MapPin[] {
    return this._pins;
  }
  private _pins: MapPin[] = [];

  /** If we got LaneModel in @Input() pins than this will contain a map with the capacity/latency */
  lanes: Map<string, Pick<LaneModel, 'Capacity' | 'Latency'>> | null = null;

  /** Emits the ID of the pin that got hovered, null if the mouse left a pin */
  @Output()
  pinHover = new EventEmitter<string | null>();

  @Output()
  pinClick = new EventEmitter<string>();

  /** @private - A {@link TrackByFunction} for all pins available in this country */
  trackPin: TrackByFunction<MapPin> = (_: number, pin: MapPin) => pin.pin.ID;

  constructor(
    private mapService: MapService,
    private cdr: ChangeDetectorRef
  ) { }
}
