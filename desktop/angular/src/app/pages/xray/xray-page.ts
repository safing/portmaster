import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, OnDestroy, OnInit, forwardRef, inject } from '@angular/core';
import { Subscription, catchError, combineLatest, of } from 'rxjs';
import { ActionIndicatorService } from '../../shared/action-indicator';
import { XrayNode, XrayService, XrayStatus, XraySubscription } from '../../shared/xray';
import { I18nService } from 'src/i18n';
import { MAP_HANDLER, CountryEvent, MapRef, MapRendererComponent } from '../spn/map-renderer/map-renderer';
import { MapPin, MapService } from '../spn/map.service';
import { XrayMapService } from './xray-map.service';

@Component({
  selector: 'app-xray-page',
  templateUrl: './xray-page.html',
  styleUrls: ['../page.scss', './xray-page.scss'],
  changeDetection: ChangeDetectionStrategy.Default,
  providers: [
    XrayMapService,
    { provide: MapService, useExisting: XrayMapService },
    { provide: MAP_HANDLER, useExisting: forwardRef(() => XrayPageComponent), multi: true },
  ],
})
export class XrayPageComponent implements OnInit, OnDestroy {
  private readonly destroyRef = inject(DestroyRef);

  private subscription = Subscription.EMPTY;
  private mapRef: MapRef | null = null;

  pins: MapPin[] = [];
  selectedPinId: string | null = null;
  showRealExitIp: boolean = false;

  toggleRealExitIp(): void {
    this.xrayMapService.showRealExitIp = this.showRealExitIp;
    this.reload();
  }
  selectedCountry: string | null = null;
  hoveredCountry: { countryCode: string; countryName: string } | null = null;
  newUrl = '';

  readonly subscriptions$ = this.xray.subscriptions$.pipe(
    catchError(err => {
      console.error('[xray] failed to load subscriptions', err);
      return of([] as XraySubscription[]);
    }),
  );

  readonly status$ = this.xray.status$.pipe(
    catchError(() => of(null as XrayStatus | null)),
  );

  get selectedPin(): MapPin | null {
    return this.pins.find(pin => pin.pin.ID === this.selectedPinId) || null;
  }

  get filteredPins(): MapPin[] {
    if (!this.selectedCountry) {
      return this.pins;
    }
    return this.pins.filter(pin => pin.entity.Country === this.selectedCountry);
  }

  get countries(): string[] {
    return Array.from(new Set(this.pins.map(pin => pin.entity.Country).filter(Boolean))).sort();
  }

  constructor(
    private readonly xray: XrayService,
    private readonly xrayMapService: XrayMapService,
    private readonly actionIndicator: ActionIndicatorService,
    private readonly i18n: I18nService,
    private readonly cdr: ChangeDetectorRef,
  ) { }

  ngOnInit(): void {
    this.destroyRef.onDestroy(() => this.subscription.unsubscribe());

    this.subscription = combineLatest([
      this.xray.nodes$.pipe(catchError(err => {
        console.error('[xray] failed to load nodes', err);
        return of([] as XrayNode[]);
      })),
      this.status$,
    ]).subscribe(([nodes, status]) => {
      this.pins = nodes.map(node => this.xrayMapService.toMapPin(node, status));
      this.xrayMapService.setPins(this.pins);
      this.renderPins(this.pins);
      this.cdr.markForCheck();
    });
  }

  ngOnDestroy(): void {
    this.subscription.unsubscribe();
    this.mapRef = null;
  }

  // ---- SPN map integration -------------------------------------------------

  registerMap(ref: MapRef): void {
    this.mapRef = ref;

    ref.onMapReady(() => {
      ref.root.append('g').attr('id', 'xray-pin-group');
      this.renderPins(this.pins);
    });

    ref.onZoomPan(() => this.positionPins());
    ref.onCountryClick(event => this.onCountryClick(event));
    ref.onCountryHover(event => this.onCountryHover(event));
  }

  unregisterMap(): void {
    this.mapRef = null;
  }

  private onCountryClick(event: CountryEvent): void {
    const wasSelected = this.selectedCountry === event.countryCode;
    this.selectedCountry = wasSelected ? null : event.countryCode;

    if (!wasSelected) {
      const countryPins = this.pins.filter(pin => pin.entity.Country === event.countryCode);
      if (countryPins.length > 0) {
        this.selectPin(countryPins[0]);
        this.connect(countryPins[0]);
      }
    }
    this.cdr.markForCheck();
  }

  private onCountryHover(event: CountryEvent | null): void {
    this.hoveredCountry = event
      ? { countryCode: event.countryCode, countryName: event.countryName }
      : null;
    this.cdr.markForCheck();
  }

  private renderPins(pins: MapPin[]): void {
    const ref = this.mapRef;
    if (!ref) {
      return;
    }

    const group = ref.select('#xray-pin-group');
    if (!group) {
      return;
    }

    const joined = group
      .selectAll<SVGGElement, MapPin>('g')
      .data(pins, pin => pin.pin.ID);

    joined.exit().remove();

    joined.enter()
      .append('g')
      .append('circle')
      .attr('class', 'marker')
      .style('cursor', 'pointer')
      .on('click', (_event: MouseEvent, pin: MapPin) => this.selectPin(pin));

    const withNodes = new Set(pins.map(pin => pin.entity.Country));
    ref.worldGroup.selectAll<SVGPathElement, any>('path')
      .attr('has-nodes', (d: any) => withNodes.has(d?.properties?.iso_a2));

    this.positionPins();
  }

  /** Re-apply positions after a zoom/pan changed the projection. */
  private positionPins(): void {
    const ref = this.mapRef;
    if (!ref) {
      return;
    }

    const group = ref.select('#xray-pin-group');
    if (!group) {
      return;
    }

    const radius = MapRendererComponent.MarkerSize / ref.zoomScale;
    group.selectAll<SVGGElement, MapPin>('g')
      .attr('transform', pin => {
        const pos = ref.projection([pin.location.Longitude, pin.location.Latitude]);
        return pos ? `translate(${pos})` : 'translate(0,0)';
      })
      .attr('is-exit', pin => String(pin.isExit))
      .attr('selected', pin => String(pin.pin.ID === this.selectedPinId));

    group.selectAll<SVGCircleElement, MapPin>('circle').attr('r', radius);
  }

  // ---- Actions -------------------------------------------------------------

  selectPin(pin: MapPin): void {
    this.selectedPinId = pin.pin.ID;
    this.positionPins();
    this.cdr.markForCheck();
  }

  connectById(pinId: string): void {
    const pin = this.pins.find(candidate => candidate.pin.ID === pinId);
    if (pin) {
      this.connect(pin);
    }
  }

  connect(pin: MapPin): void {
    this.xray.connect(pin.pin.ID).subscribe({
      next: () => this.actionIndicator.success(this.i18n.t('common.connect'), pin.pin.Name),
      error: err => this.actionIndicator.error(this.i18n.t('common.error'), err),
    });
  }

  addSubscription(): void {
    const url = this.newUrl.trim();
    if (!url) {
      return;
    }

    this.xray.addSubscription(url).subscribe({
      next: () => {
        this.newUrl = '';
        this.actionIndicator.success(this.i18n.t('xray.addSubscription'), url);
      },
      error: err => this.actionIndicator.error(this.i18n.t('common.error'), err),
    });
  }

  removeSubscription(subscription: XraySubscription): void {
    this.xray.removeSubscription(subscription.id).subscribe({
      next: () => this.actionIndicator.success(this.i18n.t('common.remove'), subscription.url),
      error: err => this.actionIndicator.error(this.i18n.t('common.error'), err),
    });
  }

  reload(): void {
    this.xray.refresh();
  }
}
