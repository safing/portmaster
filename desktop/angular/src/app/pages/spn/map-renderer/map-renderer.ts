import { AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, ElementRef, Inject, InjectionToken, Input, OnDestroy, OnInit, Optional, inject } from '@angular/core';
import { GeoPath, GeoPermissibleObjects, GeoProjection, Selection, ZoomTransform, geoMercator, geoPath, json, pointer, select, zoom, zoomIdentity } from 'd3';
import { feature } from 'topojson-client';


export type MapRoot = Selection<SVGSVGElement, unknown, null, never>;
export type WorldGroup = Selection<SVGGElement, unknown, null, unknown>

export interface CountryEvent {
  event?: MouseEvent;
  countryCode: string;
  countryName: string;
}

export interface MapRef {
  onMapReady(cb: () => any): void;
  onZoomPan(cb: () => any): void;
  onCountryHover(cb: (_: CountryEvent | null) => void): void;
  onCountryClick(cb: (_: CountryEvent) => void): void;
  select(selection: string): Selection<any, any, any, any> | null;

  countryNames: { [key: string]: string };
  root: MapRoot;
  projection: GeoProjection;
  zoomScale: number;
  worldGroup: WorldGroup;
}

export interface MapHandler {
  registerMap(ref: MapRef): void;
  unregisterMap(ref: MapRef): void;
}

export const MAP_HANDLER = new InjectionToken<MapHandler>('MAP_HANDLER');

@Component({
  // eslint-disable-next-line @angular-eslint/component-selector
  selector: 'spn-map-renderer',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: '',
  styleUrls: [
    './map-style.scss'
  ],
})
export class MapRendererComponent implements OnInit, AfterViewInit, OnDestroy {
  static readonly Rotate = 0; // so [-0, 0] is the initial center of the projection
  static readonly Maxlat = 83; // clip northern and southern pols (infinite in mercator)
  static readonly MarkerSize = 4;
  static readonly LineAnimationDuration = 200;

  private readonly destroyRef = inject(DestroyRef);
  private destroyed = false;

  countryNames: {
    [countryCode: string]: string
  } = {}

  // SVG group elements
  private svg: MapRoot | null = null;
  worldGroup!: WorldGroup;

  // Projection and line rendering functions
  projection!: GeoProjection;
  zoomScale: number = 1

  private pathFunc!: GeoPath<any, GeoPermissibleObjects>;

  get root() {
    return this.svg!
  }

  @Input()
  mapId: string = 'map'

  constructor(
    private mapRoot: ElementRef<HTMLElement>,
    private cdr: ChangeDetectorRef,
    @Inject(MAP_HANDLER) @Optional() private overlays: MapHandler[],
  ) { }

  ngOnInit(): void {
    this.overlays?.forEach(ov => {
      ov.registerMap(this)
    })

    this.cdr.detach()
  }

  select(selector: string) {
    if (!this.svg) {
      return null
    }

    return this.svg.select(selector);
  }

  private _readyCb: (() => void)[] = [];
  onMapReady(cb: () => void) {
    this._readyCb.push(cb);
  }

  private _zoomCb: (() => void)[] = [];
  onZoomPan(cb: () => void) {
    this._zoomCb.push(cb)
  }

  private _countryHoverCb: ((e: CountryEvent | null) => void)[] = [];
  onCountryHover(cb: (e: CountryEvent | null) => void) {
    this._countryHoverCb.push(cb);
  }

  private _countryClickCb: ((e: CountryEvent) => void)[] = [];
  onCountryClick(cb: (e: CountryEvent) => void) {
    this._countryClickCb.push(cb)
  }

  async ngAfterViewInit() {
    await this.renderMap()

    const observer = new ResizeObserver(() => {
      this.renderMap()
    })

    this.destroyRef.onDestroy(() => {
      observer.unobserve(this.mapRoot.nativeElement)
      observer.disconnect()
    })

    observer.observe(this.mapRoot.nativeElement);
  }

  async renderMap() {
    if (this.destroyed) {
      return;
    }

    if (!!this.svg) {
      this.svg.remove()
    }

    const map = select(this.mapRoot.nativeElement);

    // setup the basic SVG elements
    this.svg = map
      .append('svg')
      .attr('id', this.mapId)
      .attr("xmlns", "http://www.w3.org/2000/svg")
      .attr('width', '100%')
      .attr('preserveAspectRation', 'none')
      .attr('height', '100%')

    this.worldGroup = this.svg.append('g').attr('id', 'world-group')

    // load the world-map data and start rendering
    const world = await json<any>('/assets/world-50m.json');

    // actually render the countries
    const countries = (feature(world, world.objects.countries) as any);

    this.setupProjection();
    await this.setupZoom(countries);

    // we need to await the initial world render here because otherwise
    // the initial renderPins() will not be able to update the country attributes
    // and cause a delay before the state of the country (has-nodes, is-blocked, ...)
    // is visible.
    this.renderWorld(countries);

    this._readyCb.forEach(cb => cb());
  }

  ngOnDestroy() {
    this.destroyed = true;

    this.overlays?.forEach(ov => ov.unregisterMap(this));

    this._countryClickCb = [];
    this._countryHoverCb = [];
    this._readyCb = [];
    this._zoomCb = [];

    if (!this.svg) {
      return;
    }

    this.svg.remove();
    this.svg = null;
  }

  private renderWorld(countries: any) {
    // actually render the countries
    const data = countries.features;
    const self = this;

    data.forEach((country: any) => {
      this.countryNames[country.properties.iso_a2] = country.properties.name
    })
    // Add special country values.
    this.countryNames["__"] = "Anycast"

    this.worldGroup.selectAll()
      .data<GeoPermissibleObjects>(data)
      .enter()
      .append('path')
      .attr('countryCode', (d: any) => d.properties.iso_a2)
      .attr('name', (d: any) => d.properties.name)
      .attr('d', this.pathFunc)
      .on('mouseenter', function (event: MouseEvent) {
        const country = select(this).datum() as any;
        const countryEvent: CountryEvent = {
          event: event,
          countryCode: country.properties.iso_a2,
          countryName: country.properties.name,
        }

        self._countryHoverCb.forEach(cb => cb(countryEvent))
      })
      .on('mouseout', function (event: MouseEvent) {
        self._countryHoverCb.forEach(cb => cb(null))
      })
      .on('click', function (event: MouseEvent) {
        const country = select(this).datum() as any;
        const countryEvent: CountryEvent = {
          event: event,
          countryCode: country.properties.iso_a2,
          countryName: country.properties.name,
        }

        const loc = self.projection.invert!([event.clientX, event.clientY])

        console.log(loc)

        self._countryClickCb.forEach(cb => cb(countryEvent))
      })
  }

  private setupProjection() {
    const size = this.mapRoot.nativeElement.getBoundingClientRect();

    this.projection = geoMercator()
      .rotate([MapRendererComponent.Rotate, 0])
      .scale(1)
      .translate([size.width / 2, size.height / 2]);


    // path is used to update the SVG path to match our mercator projection
    this.pathFunc = geoPath().projection(this.projection);
  }

  private async setupZoom(countries: any) {
    if (!this.svg) {
      return
    }

    // create a copy of countries
    countries = {
      ...countries,
      features: [...countries.features]
    }

    // remove Antarctica from the feature set so projection.fitSize ignores it
    // and better aligns the rest of the world :)
    const aqIdx = countries.features.findIndex((p: GeoJSON.Feature) => p.properties?.iso_a2 === "AQ");
    if (aqIdx >= 0) {
      countries.features.splice(aqIdx, 1)
    }

    const size = this.mapRoot.nativeElement.getBoundingClientRect();

    this.projection.fitSize([size.width, size.height], countries)

    //this.projection.fitWidth(size.width, countries)
    //this.projection.fitHeight(size.height, countries)

    // returns the top-left and the bottom-right of the current projection
    const mercatorBounds = () => {
      const yaw = this.projection.rotate()[0];
      const xymax = this.projection([-yaw + 180 - 1e-6, -MapRendererComponent.Maxlat])!;
      const xymin = this.projection([-yaw - 180 + 1e-6, MapRendererComponent.Maxlat])!;
      return [xymin, xymax];
    }

    const s = this.projection.scale()
    const scaleExtent = [s, s * 10]

    const transform = zoomIdentity
      .scale(this.projection.scale())
      .translate(this.projection.translate()[0], this.projection.translate()[1]);

    // whenever the users zooms we need to update our groups
    // individually to apply the zoom effect.
    let tlast = {
      x: 0,
      y: 0,
      k: 0,
    }

    const self = this;

    let z = zoom<SVGSVGElement, unknown>()
      .scaleExtent(scaleExtent as [number, number])
      .on('zoom', (e) => {
        const t: ZoomTransform = e.transform;

        if (t.k != tlast.k) {
          let p = pointer(e)
          let scrollToMouse = () => { };

          if (!!p && !!p[0]) {
            const tp = this.projection.translate();
            const coords = this.projection!.invert!(p)
            scrollToMouse = () => {
              const newPos = this.projection(coords!)!;
              const yaw = this.projection.rotate()[0];
              this.projection.translate([tp[0], tp[1] + (p[1] - newPos[1])])
              this.projection.rotate([yaw + 360.0 * (p[0] - newPos[0]) / size.width * scaleExtent[0] / t.k, 0, 0])
            }
          }

          this.projection.scale(t.k);
          scrollToMouse();

        } else {
          let dy = t.y - tlast.y;
          const dx = t.x - tlast.x;
          const yaw = this.projection.rotate()[0]
          const tp = this.projection.translate();

          // use x translation to rotate based on current scale
          this.projection.rotate([yaw + 360.0 * dx / size.width * scaleExtent[0] / t.k, 0, 0])
          // use y translation to translate projection clamped to bounds
          let bounds = mercatorBounds();
          if (bounds[0][1] + dy > 0) {
            dy = -bounds[0][1];
          } else if (bounds[1][1] + dy < size.height) {
            dy = size.height - bounds[1][1];
          }
          this.projection.translate([tp[0], tp[1] + dy]);
        }

        tlast = {
          x: t.x,
          y: t.y,
          k: t.k,
        }

        // finally, re-render the SVG shapes according to the new projection
        this.worldGroup.selectAll<SVGPathElement, GeoPermissibleObjects>('path')
          .attr('d', this.pathFunc)


        this._zoomCb.forEach(cb => cb());
      });

    this.svg.call(z)
    this.svg.call(z.transform, transform);
  }

  public getCoords(lat: number, lng: number) {
    const loc = this.projection([lng, lat]);
    if (!loc) {
      return null;
    }

    const rootElem = this.mapRoot.nativeElement.getBoundingClientRect();
    const x = rootElem.x + loc[0];
    const y = rootElem.y + loc[1];

    return [x, y];
  }

  public coordsInView(lat: number, lng: number) {
    const loc = this.projection([lng, lat]);
    if (!loc) {
      return false
    }

    const rootElem = this.mapRoot.nativeElement.getBoundingClientRect();
    const x = rootElem.x + loc[0];
    const y = rootElem.y + loc[1];

    return x >= rootElem.left && x <= rootElem.right && y >= rootElem.top && y <= rootElem.bottom;
  }

}
