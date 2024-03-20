import { ChangeDetectionStrategy, ChangeDetectorRef, Component, Input, OnChanges, OnDestroy, OnInit, SimpleChanges } from '@angular/core';
import { BehaviorSubject, combineLatest, map } from 'rxjs';
import { takeWhile } from 'rxjs/operators';
import { MapPin, MapService } from './../map.service';

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: 'spn-map-country-overlay',
  templateUrl: './country-overlay.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrls: [
    './country-overlay.scss'
  ]
})
export class CountryOverlayComponent implements OnInit, OnChanges, OnDestroy {
  /** The two-letter ISO code of the country */
  @Input()
  countryCode!: string;

  /** The (english) name of the country */
  @Input()
  countryName!: string;

  /** all nodes in this country operated by Safing */
  safingNodes: MapPin[] = [];

  /** all nodes in this country operated by a community member */
  communityNodes: MapPin[] = [];

  /** used to trigger a reload onChanges */
  private reload$ = new BehaviorSubject<void>(undefined);

  constructor(
    private mapService: MapService,
    private cdr: ChangeDetectorRef,
  ) { }

  ngOnChanges(changes: SimpleChanges): void {
    this.reload$.next();
  }

  ngOnInit(): void {
    combineLatest([
      this.mapService.pins$,
      this.reload$
    ])
      .pipe(
        takeWhile(() => !this.reload$.closed),
        map(([pins]) => pins.filter(pin => pin.entity.Country === this.countryCode)),
      )
      .subscribe(pinsInCountry => {
        this.safingNodes = [];
        this.communityNodes = [];

        pinsInCountry.forEach(pin => {
          if (pin.isOffline && !pin.isActive) {
            return
          }

          if (pin.pin.VerifiedOwner === 'Safing') {
            this.safingNodes.push(pin)
          } else {
            this.communityNodes.push(pin)
          }
        })

        this.cdr.markForCheck();
      })
  }

  ngOnDestroy(): void {
    this.reload$.complete();
  }
}

