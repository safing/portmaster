import { coerceElement } from "@angular/cdk/coercion";
import { Overlay, OverlayContainer } from "@angular/cdk/overlay";
import { ComponentPortal } from '@angular/cdk/portal';
import { HttpClient } from '@angular/common/http';
import { AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, ComponentRef, DestroyRef, ElementRef, Inject, Injectable, InjectionToken, Injector, OnDestroy, OnInit, QueryList, TemplateRef, ViewChild, ViewChildren, forwardRef, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { ActivatedRoute, ParamMap, Router } from "@angular/router";
import { AppProfile, ConfigService, Connection, ExpertiseLevel, FeatureID, Netquery, PORTMASTER_HTTP_API_ENDPOINT, PortapiService, SPNService, SPNStatus, UserProfile } from "@safing/portmaster-api";
import { SfngDialogService } from "@safing/ui";
import { Line as D3Line, Selection, interpolateString, line, select } from 'd3';
import { BehaviorSubject, Observable, Subscription, combineLatest, interval, of } from "rxjs";
import { catchError, debounceTime, map, mergeMap, share, startWith, switchMap, take, takeUntil, withLatestFrom } from "rxjs/operators";
import { fadeInAnimation, fadeInListAnimation, fadeOutAnimation } from "src/app/shared/animations";
import { ExpertiseService } from "src/app/shared/expertise/expertise.service";
import { SPNAccountDetailsComponent } from "src/app/shared/spn-account-details";
import { CountryDetailsComponent } from "./country-details";
import { CountryEvent, MAP_HANDLER, MapRef, MapRendererComponent } from "./map-renderer/map-renderer";
import { MapPin, MapService } from "./map.service";
import { PinDetailsComponent } from "./pin-details";
import { PinOverlayComponent } from "./pin-overlay";
import { OVERLAY_REF } from './utils';

export const MapOverlay = new InjectionToken<Overlay>('MAP_OVERLAY')

export type PinGroup = Selection<SVGGElement, unknown, null, unknown>;
export type LaneGroup = Selection<SVGGElement, unknown, null, unknown>;

export interface Path {
  id: string;
  points: (MapPin | [number, number])[];
  attributes?: {
    [key: string]: string;
  }
}

export interface PinEvent {
  event?: MouseEvent;
  mapPin: MapPin;
}


/**
 * A custom class that implements the OverlayContainer interface of CDK. This
 * is used so we can configure a custom container element that will hold all overlays created
 * by the map component. This way the overlays will be bound to the map container and not overflow
 * the sidebar or other overlays that are created by the "root" app.
 */
@Injectable()
class MapOverlayContainer {
  private _overlayContainer?: HTMLElement;

  setOverlayContainer(element: ElementRef<HTMLElement> | HTMLElement) {
    this._overlayContainer = coerceElement(element);
  }

  getContainerElement(): HTMLElement {
    if (!this._overlayContainer) {
      throw new Error("Overlay container element not initialized. Call setOverlayContainer first.")
    }

    return this._overlayContainer;
  }
}

@Component({
  templateUrl: './spn-page.html',
  styleUrls: ['./spn-page.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
  providers: [
    MapOverlayContainer,
    { provide: MapOverlay, useClass: Overlay },
    { provide: OverlayContainer, useExisting: MapOverlayContainer },
    { provide: MAP_HANDLER, useExisting: forwardRef(() => SpnPageComponent), multi: true }
  ],
  animations: [
    fadeInListAnimation,
    fadeInAnimation,
    fadeOutAnimation
  ]
})
export class SpnPageComponent implements OnInit, OnDestroy, AfterViewInit {
  private destroyRef = inject(DestroyRef);

  private countryDebounceTimer: any | null = null;

  /** a list of opened country details. required to close them on destry */
  private openedCountryDetails: CountryDetailsComponent[] = [];

  readonly featureID = FeatureID.SPN;

  paths: Path[] = [];

  @ViewChild('overlayContainer', { static: true, read: ElementRef })
  overlayContainer!: ElementRef<HTMLElement>;

  @ViewChild(MapRendererComponent, { static: true })
  mapRenderer!: MapRendererComponent;

  @ViewChild('accountDetails', { read: TemplateRef, static: true })
  accountDetails: TemplateRef<any> | null = null;

  /** A list of pro-tip templates in our view */
  @ViewChildren('proTip', { read: TemplateRef })
  proTipTemplates!: QueryList<TemplateRef<any>>;

  /** The selected pro-tip template */
  proTipTemplate: TemplateRef<any> | null = null;

  /** currentUser holds the current SPN user profile if any */
  currentUser: UserProfile | null = null;

  /** An observable that emits all active processes. */
  activeProfiles$: Observable<AppProfile[]>;

  /** Whether or not we are still waiting for all data in order to satisfy a "show process/pin" request by query-params */
  loading = true;

  /** a list of currently selected pins */
  selectedPins: PinOverlayComponent[] = [];

  /** the currently hovered country, if any */
  hoveredCountry: {
    countryName: string;
    countryCode: string;
  } | null = null;

  liveMode = false;
  liveModePaths: Path[] = [];

  private liveModeSubscription = Subscription.EMPTY;

  /**
   * spnStatusTranslation translates the spn status to the text that is displayed
   * at the view
   */
  readonly spnStatusTranslation: Readonly<Record<SPNStatus['Status'], string>> = {
    connected: 'Connected',
    connecting: 'Connecting',
    disabled: 'Disabled',
    failed: 'Failure'
  }


  private mapRef: MapRef | null = null;
  private lineFunc: D3Line<(MapPin | [number, number])> | null = null;
  private highlightedPins = new Set<string>();

  registerMap(ref: MapRef) {
    this.mapRef = ref;

    ref.onMapReady(() => {
      // we want to have straight lines between our hubs so we use a custom
      // path function that updates x and y coordinates based on the mercator projection
      // without, points will no be at the correct geo-coordinates.
      this.lineFunc = line<MapPin | [number, number]>()
        .x(d => {
          if (Array.isArray(d)) {
            return this.mapRef!.projection([d[0], d[1]])![0];
          }
          return this.mapRef!.projection([d.location.Longitude, d.location.Latitude])![0];
        })
        .y(d => {
          if (Array.isArray(d)) {
            return this.mapRef!.projection([d[0], d[1]])![1];
          }
          return this.mapRef!.projection([d.location.Longitude, d.location.Latitude])![1];
        })

      this.mapRef!.root.append('g').attr('id', 'line-group')
      this.mapRef!.root.append('g').attr('id', 'pin-group')

      if (this.mapService._pins$.getValue().length > 0) {
        this.renderPins(this.mapService._pins$.getValue())
      }
    })

    ref.onCountryClick(event => this.onCountryClick(event))
    ref.onCountryHover(event => this.onCountryHover(event))
    ref.onZoomPan(() => this.onZoomAndPan())
  }

  unregisterMap(ref: MapRef) {
    this.mapRef = null;
    this.lineFunc = null;
  }

  constructor(
    private configService: ConfigService,
    private spnService: SPNService,
    private netquery: Netquery,
    private expertiseService: ExpertiseService,
    private router: Router,
    private route: ActivatedRoute,
    private portapi: PortapiService,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) private httpAPI: string,
    private http: HttpClient,
    public mapService: MapService,
    @Inject(MapOverlay) private mapOverlay: Overlay,
    private dialog: SfngDialogService,
    private overlayContainerService: MapOverlayContainer,
    private cdr: ChangeDetectorRef,
    private injector: Injector,
  ) {
    this.activeProfiles$ = interval(5000)
      .pipe(
        startWith(-1),
        switchMap(() => this.netquery.getActiveProfiles()),
        share({ connector: () => new BehaviorSubject<AppProfile[]>([]) })
      )
  }

  ngAfterViewInit() {
    // configure our custom overlay container
    this.overlayContainerService.setOverlayContainer(this.overlayContainer);

    // Select a random "Pro-Tip" template and run change detection
    this.proTipTemplate = this.proTipTemplates.get(Math.floor(Math.random() * this.proTipTemplates.length)) || null;
    this.cdr.detectChanges();
  }

  openAccountDetails() {
    this.dialog.create(SPNAccountDetailsComponent, {
      autoclose: true,
      backdrop: 'light'
    })
  }

  ngOnInit() {
    this.spnService
      .profile$
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        catchError(() => of(null))
      )
      .subscribe((user: UserProfile | null) => {
        if (user?.state !== '') {
          this.currentUser = user || null;
        } else {
          this.currentUser = null;
        }

        this.cdr.markForCheck();
      })

    let previousQueryMap: ParamMap | null = null;

    combineLatest([
      this.route.queryParamMap,
      this.mapService.pins$,
      this.activeProfiles$,
    ])
      .pipe(
        takeUntilDestroyed(this.destroyRef),
      ).subscribe(([params, pins, profiles]) => {
        if (params !== previousQueryMap) {
          const app = params.get("app")
          if (!!app) {
            const profile = profiles.find(p => `${p.Source}/${p.ID}` === app);
            if (!!profile) {
              const pinID = params.get("pin")
              const pin = pins.find(p => p.pin.ID === pinID);

              this.selectGroup(profile, pin)
            }
          }

          previousQueryMap = params;
        }

        this.renderPins(pins);

        // we're done with everything now.
        this.loading = false;
      })

  }

  toggleLiveMode(enabled: boolean) {
    this.liveMode = enabled;

    if (!enabled) {
      this.liveModeSubscription.unsubscribe();
      this.liveModePaths = [];
      this.updatePaths([]);
      this.cdr.markForCheck();

      return;
    }

    this.liveModeSubscription = this.portapi.watchAll<Connection>("network:tree")
      .pipe(
        withLatestFrom(this.mapService.pinsMap$),
        takeUntilDestroyed(this.destroyRef),
        debounceTime(100),
      )
      .subscribe(([connections, mapPins]) => {
        connections = connections.filter(conn => conn.Ended === 0 && !!conn.TunnelContext);

        this.liveModePaths = connections.map(conn => {
          const points: (MapPin | [number, number])[] = conn.TunnelContext!.Path.map(hop => mapPins.get(hop.ID)!)

          if (!!conn.Entity.Coordinates) {
            points.push([conn.Entity.Coordinates.Longitude, conn.Entity.Coordinates.Latitude])
          }

          return {
            id: conn.Entity.Domain || conn.ID,
            points: points,
            attributes: {
              'is-live': 'true',
              'is-encrypted': `${conn.Encrypted}`
            }
          }
        })

        this.updatePaths([])
        this.cdr.markForCheck();
      })
  }

  /**
   * Toggle the spn/enable setting. This does NOT update the view as that
   * will happen as soon as we get an update from the db qsub.
   *
   * @private - template only
   */
  toggleSPN() {
    this.configService.get('spn/enable')
      .pipe(
        map(setting => setting.Value ?? setting.DefaultValue),
        mergeMap(active => this.configService.save('spn/enable', !active))
      )
      .subscribe()
  }

  /**
   * Select one or more pins by ID. If shift key is hold then all currently
   * selected pin overlays will be cleared before selecting the new ones.
   */
  private selectPins(event: MouseEvent | undefined, pinIDs: Observable<string[]>) {
    combineLatest([
      this.mapService.pins$,
      pinIDs,
    ])
      .pipe(take(1))
      .subscribe(([allPins, pinIDs]) => {
        if (event?.shiftKey !== true) {
          this.selectedPins
            .filter(overlay => !overlay.hasBeenMoved)
            .forEach(selected => selected.disposeOverlay())
        }

        pinIDs
          .filter(id => !this.selectedPins.find(selectedPin => selectedPin.mapPin.pin.ID === id))
          .map(id => allPins.find(pin => pin.pin.ID === id))
          .filter(mapPin => !!mapPin)
          .forEach(mapPin => this.onPinClick({
            mapPin: mapPin!,
          }));
      })
  }

  /**
   * Select all pins that are used for transit.
   *
   * @private - template only
   */
  selectTransitNodes(event: MouseEvent) {
    this.selectPins(event, this.mapService.getPinIDsWithActiveSession())
  }

  /**
   * Select all pins that are used as an exit hub.
   *
   * @private - template only
   */
  selectExitNodes(event: MouseEvent) {
    this.selectPins(event, this.mapService.getPinIDsUsedAsExit())
  }

  /**
   * Select all pins that currently host alive connections.
   *
   * @private - template only
   */
  selectNodesWithAliveConnections(event: MouseEvent) {
    this.selectPins(event, this.mapService.getPinIDsWithActiveConnections())
  }

  navigateToMonitor(process: AppProfile) {
    this.router.navigate(['/app', process.Source, process.ID])
  }

  ngOnDestroy() {
    this.openedCountryDetails.forEach(cmp => cmp.dialogRef!.close());
  }

  onZoomAndPan() {
    this.updateOverlayPositions();

    if (this.mapRef) {
      this.mapRef.root
        .select('#lines-group')
        .selectAll<SVGPathElement, Path>('path')
        .attr('d', d => this.lineFunc!(d.points))

      this.mapRef.root
        .select("#pin-group")
        .selectAll<SVGGElement, MapPin>('g')
        .attr('transform', d => `translate(${this.mapRef!.projection([d.location.Longitude, d.location.Latitude])})`)
    }

    this.cdr.markForCheck();
  }

  private createPinOverlay(pinEvent: PinEvent, lm: Map<string, MapPin>): PinOverlayComponent {
    const paths = this.getRouteHome(pinEvent.mapPin, lm, false)
    const overlayBoundingRect = this.overlayContainer.nativeElement.getBoundingClientRect();
    const target = pinEvent.event?.target || this.getPinElem(pinEvent.mapPin.pin.ID)?.children[0];
    let delay = 0;
    if (paths.length > 0) {
      delay = paths[0].points.length * MapRendererComponent.LineAnimationDuration;
    }

    const overlayRef = this.mapOverlay.create({
      positionStrategy: this.mapOverlay.position()
        .flexibleConnectedTo(new ElementRef(target))
        .withDefaultOffsetY(-overlayBoundingRect.y - 10)
        .withDefaultOffsetX(-overlayBoundingRect.x + 20)
        .withPositions([
          {
            overlayX: 'start',
            overlayY: 'top',
            originX: 'start',
            originY: 'top'
          }
        ]),
      scrollStrategy: this.mapOverlay.scrollStrategies.reposition(),
    })

    const injector = Injector.create({
      providers: [
        {
          provide: OVERLAY_REF,
          useValue: overlayRef,
        }
      ],
      parent: this.injector
    })


    const pinOverlay = overlayRef.attach(
      new ComponentPortal(PinOverlayComponent, undefined, injector)
    ).instance;

    pinOverlay.delay = delay;
    pinOverlay.mapPin = pinEvent.mapPin;
    if (paths.length > 0) {
      pinOverlay.routeHome = {
        ...(paths[0]),
      }
      pinOverlay.additionalPaths = paths.slice(1);
    }

    return pinOverlay;
  }


  private openPinDetails(id: string) {
    this.dialog.create(PinDetailsComponent, {
      data: id,
      backdrop: false,
      autoclose: true,
      dragable: true,
    })
  }

  private openCountryDetails(event: CountryEvent) {
    // abort if we already have the country details open.
    if (this.openedCountryDetails.find(cmp => cmp.countryCode === event.countryCode)) {
      return;
    }

    const ref = this.dialog.create(CountryDetailsComponent, {
      data: {
        name: event.countryName,
        code: event.countryCode,
      },
      autoclose: false,
      dragable: true,
      backdrop: false,
    })
    const component = (ref.contentRef() as ComponentRef<CountryDetailsComponent>).instance;

    // used to track whether we highlighted a map pin
    let hasPinHighlightActive = false;

    combineLatest([
      component.pinHover,
      this.mapService.pins$,
    ])
      .pipe(
        takeUntil(ref.onClose),
      )
      .subscribe(([hovered, pins]) => {
        hasPinHighlightActive = hovered !== null;

        if (hovered !== null) {
          this.onPinHover({
            mapPin: pins.find(p => p.pin.ID === hovered)!,
          })
          this.highlightPin(hovered, true)
        } else {
          this.onPinHover(null);
          this.clearPinHighlights();
        }


        this.cdr.markForCheck();
      })

    ref.onClose
      .subscribe(() => {
        if (hasPinHighlightActive) {
          this.clearPinHighlights();
        }

        const index = this.openedCountryDetails.findIndex(cmp => cmp === component);
        if (index >= 0) {
          this.openedCountryDetails.splice(index, 1);
        }
      })

    this.openedCountryDetails.push(component);
  }

  private updateOverlayPositions() {
    this.mapService.pinsMap$
      .pipe(take(1))
      .subscribe(allPins => {
        this.selectedPins.forEach(pin => {
          const pinObj = allPins.get(pin.mapPin.pin.ID);
          if (!pinObj) {
            return;
          }

          pin.overlayRef.updatePosition();
        })
      })
  }

  onCountryClick(countryEvent: CountryEvent) {
    this.openCountryDetails(countryEvent);
  }

  onCountryHover(countryEvent: CountryEvent | null) {
    if (this.countryDebounceTimer !== null) {
      clearTimeout(this.countryDebounceTimer);
    }

    if (!!countryEvent) {
      this.hoveredCountry = {
        countryCode: countryEvent.countryCode,
        countryName: countryEvent.countryName,
      }
      this.cdr.markForCheck();

      return;
    }

    this.countryDebounceTimer = setTimeout(() => {
      this.hoveredCountry = null;
      this.countryDebounceTimer = null;
      this.cdr.markForCheck();
    }, 200)
  }

  onPinClick(pinEvent: PinEvent) {
    // if the control key hold when clicking a map pin, we immediately open the
    // pin details instead of the overlay.
    if (pinEvent.event?.ctrlKey) {
      this.openPinDetails(pinEvent.mapPin.pin.ID);
    }

    const overlay = this.selectedPins.find(por => por.mapPin.pin.ID === pinEvent.mapPin.pin.ID);
    if (!!overlay) {
      overlay.disposeOverlay()
      return;
    }

    // if shiftKey was not pressed during the pinClick we dispose all active overlays that have not been
    // moved by the user
    if (!pinEvent.event?.shiftKey) {
      this.selectedPins
        .filter(overlay => !overlay.hasBeenMoved)
        .forEach(selected => selected.disposeOverlay())
    }

    this.mapService.pinsMap$
      .pipe(take(1))
      .subscribe(async lm => {
        const overlayComp = this.createPinOverlay(pinEvent, lm);

        // when the user wants to dispose a pin overlay (by clicking the X) we
        //  - make sure the pin is not highlighted anymore
        //  - remove the pin from the selectedPins list
        //  - remove lines showing the route to the home hub
        overlayComp.afterDispose
          .subscribe(pinID => {
            this.highlightPin(pinID, false);

            const overlayIdx = this.selectedPins.findIndex(por => por.mapPin.pin.ID === pinEvent.mapPin.pin.ID);
            this.selectedPins.splice(overlayIdx, 1)

            this.updatePaths()
            this.cdr.markForCheck();
          })

        // when the user hovers/leaves a pin overlay, we:
        //   - move the pin-overlay to the top when the user hovers it so stacking order is correct
        //   - (un)hightlight the pin element on the map
        overlayComp.overlayHover
          .subscribe(evt => {
            this.highlightPin(evt.pinID, evt.type === 'enter')

            // over the overlay component to the top
            if (evt.type === 'enter') {
              this.selectedPins.forEach(ref => {
                if (ref !== overlayComp && ref.overlayRef.hostElement) {
                  ref.overlayRef.hostElement.style.zIndex = '0';
                }
              })

              overlayComp.overlayRef.hostElement.style.zIndex = '';
            }
          })

        this.selectedPins.push(overlayComp)

        this.updatePaths([]);
        this.cdr.markForCheck();
      })
  }

  private updatePaths(additional: Path[] = []) {
    const paths = [
      ...(this.selectedPins
        .reduce((list, pin) => {
          if (pin.routeHome) {
            list.push(pin.routeHome)
          }

          return [
            ...list,
            ...(pin.additionalPaths || [])
          ]
        }, [] as Path[])),
      ...this.liveModePaths,
      ...additional
    ]

    this.paths = paths.map(p => {
      return {
        ...p,
        attributes: {
          class: 'lane',
          ...(p.attributes || {})
        }
      }
    });

    this.renderPaths(this.paths)
  }

  onPinHover(pinEvent: PinEvent | null) {
    if (!pinEvent) {
      this.updatePaths([]);
      this.onCountryHover(null);

      return;
    }

    // we also emit a country hover event here to keep the country
    // overlay open.
    const countryName = this.mapRenderer.countryNames[pinEvent.mapPin.entity.Country]
    this.onCountryHover({
      event: pinEvent.event,
      countryCode: pinEvent.mapPin.entity.Country,
      countryName: countryName!,
    })

    // in developer mode, we show all connected lanes of the hovered pin.
    if (this.expertiseService.currentLevel === ExpertiseLevel.Developer) {
      this.mapService.pinsMap$
        .pipe(take(1))
        .subscribe(lm => {
          const lanes = this.getConnectedLanes(pinEvent?.mapPin, lm)
          this.updatePaths(lanes);
          this.cdr.markForCheck();
        })
    }
  }

  /**
   * Marks a process group as selected and either selects one or all exit pins
   * of that group. If shiftKey is pressed during click, the ID(s) will be added
   * to the list of selected pins instead of replacing it. If shiftKey is pressed
   * the process group itself will NOT be displayed as selected.
   *
   * @private - template only
   */
  selectGroup(grp: AppProfile, pin?: MapPin | null, event?: MouseEvent) {
    if (!!pin) {
      this.selectPins(event, of([pin.pin.ID]))
      return;
    }

    this.selectPins(event, this.mapService.getExitPinIDsForProfile(grp))
  }

  /** Returns a list of lines that represent the route from pin to home. */
  private getRouteHome(pin: MapPin, lm: Map<string, MapPin>, includeAllRoutes = false): Path[] {
    let pinsToEval: MapPin[] = [pin];

    // decide whether to draw all connection routes that travel through pin.
    if (includeAllRoutes) {
      pinsToEval = [
        ...pinsToEval,
        ...Array.from(lm.values())
          .filter(p => p.pin.Route?.includes(pin.pin.ID))
      ]
    }

    return pinsToEval.map(pin => ({
      id: `route-home-from-${pin.pin.ID}`,
      points: (pin.pin.Route || []).map(hop => lm.get(hop)!),
      attributes: {
        'in-use': 'true'
      }
    }));
  }

  /** Returns a list of lines the represent all lanes to connected pins of pin */
  private getConnectedLanes(pin: MapPin, lm: Map<string, MapPin>): Path[] {
    let result: Path[] = [];

    // add all lanes for connected hubs
    Object.keys(pin.pin.ConnectedTo).forEach(target => {
      const p = lm.get(target);
      if (!!p) {
        result.push({
          id: lineID([pin, p]),
          points: [
            pin,
            p
          ]
        })
      }
    });

    return result;

  }

  private async renderPaths(paths: Path[]) {
    if (!this.mapRef) {
      return;
    }

    const ref = this.mapRef!

    const linesGroup: LaneGroup = this.mapRef.select("#line-group")!

    const self = this;
    const renderedPaths = linesGroup.selectAll<SVGPathElement, Path>('path')
      .data(paths, p => p.id);

    renderedPaths
      .enter()
      .append('path')
      .attr('d', path => {
        return self.lineFunc!(path.points)
      })
      .attr("stroke-width", d => {
        if (d.attributes) {
          if (d.attributes['in-use']) {
            return 2 / ref.zoomScale
          }
        }

        return 1 / ref.zoomScale;
      })
      .call(sel => {
        if (sel.empty()) {
          return;
        }
        const data = sel.datum()?.attributes || {};
        Object.keys(data)
          .forEach(key => {
            sel.attr(key, data[key])
          })
      })
      .transition("enter-lane")
      .duration(d => d.points.length * MapRendererComponent.LineAnimationDuration)
      .attrTween('stroke-dasharray', tweenDashEnter)

    renderedPaths.exit()
      .interrupt("enter-lane")
      .transition("leave-lane")
      .duration(200)
      .attrTween('stroke-dasharray', tweenDashExit)
      .remove();
  }

  private async renderPins(pins: MapPin[]) {
    pins = pins.filter(pin => !pin.isOffline || pin.isActive);

    if (!this.mapRef) {
      return
    }

    const ref = this.mapRef!;

    const countriesWithNodes = new Set<string>();

    pins.forEach(pin => {
      countriesWithNodes.add(pin.entity.Country)
    })

    const pinsGroup = ref.select('#pin-group')!

    const pinElements = pinsGroup
      .selectAll<SVGGElement, MapPin>('g')
      .data(pins, pin => pin.pin.ID)

    const self = this;

    // add new pins
    pinElements
      .enter()
      .append('g')
      .append(d => {
        const val = MapRendererComponent.MarkerSize / ref.zoomScale;

        if (d.isHome) {
          const homeIcon = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
          homeIcon.setAttribute('r', `${val * 1.25}`)

          return homeIcon;
        }

        if (d.pin.VerifiedOwner === 'Safing') {
          const polygon = document.createElementNS('http://www.w3.org/2000/svg', 'polygon')
          polygon.setAttribute('points', `0,-${val} -${val},${val} ${val},${val}`)

          return polygon;
        }

        const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle')
        circle.setAttribute('r', `${val}`)

        return circle;
      })
      .attr("stroke-width", d => {
        if (d.isExit || self.highlightedPins.has(d.pin.ID)) {
          return 2 / ref.zoomScale
        }

        if (d.isHome) {
          return 4.5 / ref.zoomScale
        }

        return 1 / ref.zoomScale
      })
      .call(selection => {
        selection
          .style('opacity', 0)
          .attr('transform', d => 'scale(0)')
          .transition('enter-marker')
          /**/.duration(1000)
          /**/.attr('transform', d => `scale(1)`)
          /**/.style('opacity', 1)
      })
      .on('click', function (e: MouseEvent) {
        const pin = select(this).datum() as MapPin;
        self.onPinClick({
          event: e,
          mapPin: pin
        });
      })
      .on('mouseenter', function (e: MouseEvent) {
        const pin = select(this).datum() as MapPin;
        self.onPinHover({
          event: e,
          mapPin: pin,
        })
      })
      .on('mouseout', function (e: MouseEvent) {
        self.onPinHover(null);
      })

    // remove pins from the map that disappeared
    pinElements
      .exit()
      .remove()

    // update all pins to their correct position and update their attributes
    pinsGroup.selectAll<SVGGElement, MapPin>('g')
      .attr('hub-id', d => d.pin.ID)
      .attr('is-home', d => d.isHome)
      .attr('transform', d => `translate(${ref.projection([d.location.Longitude, d.location.Latitude])})`)
      .attr('in-use', d => d.isTransit)
      .attr('is-exit', d => d.isExit)
      .attr('raise', d => this.highlightedPins.has(d.pin.ID))

    // update the attributes of the country shapes
    ref.worldGroup.selectAll<SVGGElement, any>('path')
      .attr('has-nodes', d => countriesWithNodes.has(d.properties.iso_a2))

    // get all in-use pins and raise them to the top
    pinsGroup.selectAll<SVGGElement, MapPin>('g[in-use=true]')
      .raise()

    // finally, re-raise all pins that are highlighted
    pinsGroup.selectAll<SVGGElement, MapPin>('g[raise=true]')
      .raise()

    const activeCountrySet = new Set<string>();
    pins.forEach(pin => {
      if (pin.isTransit) {
        activeCountrySet.add(pin.pin.ID)
      }
    })

    // update the in-use attributes of the country shapes
    ref.worldGroup.selectAll<SVGPathElement, any>('path')
      .attr('in-use', d => activeCountrySet.has(d.properties.iso_a2))

    this.cdr.detectChanges();
  }

  public getPinElem(pinID: string) {
    if (!this.mapRef) {
      return
    }

    return this.mapRef.root
      .select("#pin-group")
      .select<SVGGElement>(`g[hub-id=${pinID}]`)
      .node()
  }

  public clearPinHighlights() {
    if (!this.mapRef) {
      return
    }

    this.mapRef.root
      .select('#pin-group')
      .select<SVGGElement>(`g[raise=true]`)
      .attr('raise', false)

    this.highlightedPins.clear();
  }

  public highlightPin(pinID: string, highlight: boolean) {
    if (highlight) {
      this.highlightedPins.add(pinID)
    } else {
      this.highlightedPins.delete(pinID);
    }

    if (!this.mapRef) {
      return
    }
    const pinElemn = this.mapRef!.root
      .select("#pin-group")
      .select<SVGGElement>(`g[hub-id=${pinID}]`)
      .attr('raise', highlight)

    if (highlight) {
      pinElemn
        .raise()
    }
  }
}

function lineID(l: [MapPin, MapPin]): string {
  return [l[0].pin.ID, l[1].pin.ID].sort().join("-")
}

const tweenDashEnter = function (this: SVGPathElement) {
  const len = this.getTotalLength();
  const interpolate = interpolateString(`0, ${len}`, `${len}, ${len}`);
  return (t: number) => {
    if (t === 1) {
      return '0';
    }
    return interpolate(t);
  }
}

const tweenDashExit = function (this: SVGPathElement) {
  const len = this.getTotalLength();
  const interpolate = interpolateString(`${len}, ${len}`, `0, ${len}`);
  return (t: number) => {
    if (t === 1) {
      return `${len}`;
    }
    return interpolate(t);
  }
}
