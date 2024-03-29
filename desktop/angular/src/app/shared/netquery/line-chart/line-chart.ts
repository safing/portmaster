import { coerceBooleanProperty, coerceNumberProperty, coerceStringArray } from '@angular/cdk/coercion';
import { AfterViewInit, ChangeDetectionStrategy, Component, DestroyRef, ElementRef, Input, OnChanges, OnInit, SimpleChanges, inject } from '@angular/core';
import { BandwidthChartResult, ChartResult } from '@safing/portmaster-api';
import * as d3 from 'd3';
import { Selection } from 'd3';
import { AppComponent } from 'src/app/app.component';
import { formatDuration, timeAgo } from '../../pipes';
import { objKeys } from '../../utils';
import { BytesPipe } from '../../pipes/bytes.pipe';

export interface SeriesConfig {
  lineColor: string;
  areaColor?: string;
}

export interface Marker {
  text: string;
  time: Date | number | string;
}

export interface ChartConfig<T extends SeriesData> {
  series: {
    [key in Exclude<keyof T, 'timestamp'>]?: SeriesConfig;
  },
  time?: {
    from: number | string | Date;
    to?: number | string | Date;
  },
  fromMargin?: number;
  toMargin?: number;
  valueFormat?: (n: d3.NumberValue, seriesKey?: string) => string,
  tooltipFormat?: (data: T) => string;
  timeFormat?: (n: Date) => string,
  showDataPoints?: boolean;
  fillEmptyTicks?: {
    interval: number;
  },
  verticalMarkers?: Marker[];
}

function coerceDate(d: Date | number | string): Date {
  if (typeof d === 'string') {
    return new Date(d)
  }

  if (d instanceof Date) {
    return d
  }

  if (d < 0) {
    return new Date((new Date()).getTime() + d * 1000)
  }

  return new Date(d * 1000);
}

export const DefaultChartConfig: ChartConfig<ChartResult> = {
  series: {
    value: {
      lineColor: 'text-green-200',
      areaColor: 'text-green-100 text-opacity-25'
    },
    countBlocked: {
      lineColor: 'text-red-200',
      areaColor: 'text-red-100 text-opacity-25'
    }
  },
}

export const DefaultBandwidthChartConfig: ChartConfig<BandwidthChartResult<any>> = {
  series: {
    outgoing: {
      lineColor: 'text-deepPurple-500',
      areaColor: 'text-deepPurple-700 text-opacity-5',
    },
    incoming: {
      lineColor: 'text-cyan-800',
      areaColor: 'text-cyan-700 text-opacity-5',
    },
  },
  time: {
    from: -10 * 60,
  },
  valueFormat: (n: d3.NumberValue, seriesKey?: string) => {
    let prefix = '';
    if (seriesKey !== undefined) {
      prefix = seriesKey === 'incoming' ? 'Received: ' : 'Sent: '
    }
    return prefix + new BytesPipe().transform(n.valueOf())
  },
  timeFormat: (n: Date) => {
    const diff = Math.floor(new Date().getTime() - n.getTime())
    return formatDuration(diff, false, true) + " ago"
  },
  tooltipFormat: (n: BandwidthChartResult<any>) => {
    const bytes = new BytesPipe().transform
    const received = `Received: ${bytes(n?.incoming || 0)}`;
    const sent = `Sent: ${bytes(n?.outgoing || 0)}`

    if ((n?.incoming || 0) > (n?.outgoing || 0)) {
      return `${received}\n${sent}`
    }
    return `${sent}\n${received}`
  },
  showDataPoints: true,
  fillEmptyTicks: {
    interval: 60
  },
}

export interface SeriesData {
  timestamp: number;
}

@Component({
  selector: 'sfng-netquery-line-chart',
  styles: [
    `
    :host {
      @apply block h-full w-full;
    }
    `
  ],
  template: '',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngNetqueryLineChartComponent<D extends SeriesData = any> implements OnChanges, OnInit, AfterViewInit {
  private destroyRef = inject(DestroyRef);

  @Input()
  data: D[] = [];

  private preparedData: D[] = [];

  private width = 700;
  private height = 250;

  @Input()
  set margin(v: any) {
    this._margin = coerceNumberProperty(v);
  }
  get margin() { return this._margin; }
  private _margin = 0;

  @Input()
  config!: ChartConfig<D>;

  svg!: Selection<any, any, any, any>;
  svgInner!: Selection<SVGGElement, any, any, any>;
  yScale!: d3.ScaleLinear<number, number, never>;
  xScale!: d3.ScaleTime<number, number, never>;
  xAxis!: Selection<SVGGElement, any, any, any>;
  yAxis!: Selection<SVGGElement, any, any, any>;

  @Input()
  set showAxis(v: any) {
    this._showAxis = coerceBooleanProperty(v);
  }
  get showAxis() {
    return this._showAxis;
  }
  private _showAxis = true;

  constructor(
    public chartElem: ElementRef,
    private app: AppComponent
  ) { }

  ngOnInit() {
    if (!this.config) {
      this.config = DefaultChartConfig as any;
    }

    const observer = new ResizeObserver(() => {
      this.redraw();
    })

    observer.observe(this.chartElem.nativeElement)

    this.destroyRef.onDestroy(() => observer.disconnect())

  }

  ngAfterViewInit(): void {
    requestAnimationFrame(() => {
      this.redraw()
    })
  }

  ngOnChanges(changes: SimpleChanges): void {
    if (Object.prototype.hasOwnProperty.call(changes, 'config') && this.config) {
      this.redraw()
      return
    }

    if (Object.prototype.hasOwnProperty.call(changes, 'data') && this.data) {
      this.drawChart();
    }
  }

  get yMargin() {
    if (this.showAxis) {
      return 16;
    }
    return 0;
  }

  redraw(event?: Event) {
    if (!!this.svg) {
      this.svg.remove();
    }

    this.initializeChart();
    this.drawChart();
  }

  private initializeChart(): void {
    this.width = this.chartElem.nativeElement.getBoundingClientRect().width;
    this.height = this.chartElem.nativeElement.getBoundingClientRect().height;

    this.svg = d3
      .select(this.chartElem.nativeElement)
      .append('svg')

    this.svg.attr('width', this.width);
    this.svg.attr('height', this.height);

    this.svgInner = this.svg
      .append('g')
      .attr('height', '100%');

    this.yScale = d3
      .scaleLinear()

    this.xScale = d3.scaleTime();

    // setup event handlers to higlight the closest data points
    let lastClosestIndex = -1;

    if (this.config.showDataPoints) {
      const self = this;
      this.svg
        .on("mousemove", function (event: MouseEvent) {
          let x = d3.pointer(event)[0];

          let closest = self.data.reduce((best, value, idx) => {
            let absx = Math.abs(self.xScale(new Date(value.timestamp * 1000)) - x);
            if (absx < best.value) {
              return { index: idx, value: absx, timestamp: self.data[idx].timestamp }
            }

            return best

          }, { index: 0, value: Number.MAX_SAFE_INTEGER, timestamp: 0 })

          if (lastClosestIndex === closest.index) {
            return;
          }
          lastClosestIndex = closest.index;

          if (self.config.tooltipFormat) {
            // append a title to the parent SVG, this is a quick-fix for showing some
            // information on the highlighted points
            // TODO(ppacher): actually render a nice tooltip there.
            let tooltip = self.svg
              .select<HTMLTitleElement>('title.tooltip')

            if (tooltip.empty()) {
              tooltip = self.svg.append("title")
                .attr("class", "tooltip")
            }

            tooltip
              .text(self.config.tooltipFormat!(self.data[closest.index]))
          }

          self.svgInner
            .select(".vertical-marker")
            .selectAll(".mouse-position")
            .remove()

          self.svgInner
            .select(".vertical-marker")
            .append("line")
            .classed("mouse-position", true)
            .attr("x1", d => self.xScale(closest.timestamp * 1000))
            .attr("y1", -10)
            .attr("x2", d => self.xScale(closest.timestamp * 1000))
            .attr("y2", self.height - self.yMargin)
            .classed("text-secondary text-opacity-50", true)
            .attr("stroke", "currentColor")
            .attr("stroke-width", 1)
            .attr("stroke-dasharray", 2)

          self.svgInner
            .select(".points")
            .selectAll<SVGCircleElement, [number, number]>("circle")
            .classed("opacity-100", d => self.xScale.invert(d[0]).getTime() === closest.timestamp * 1000)
        })
        .on("mouseleave", function () {
          lastClosestIndex = -1;

          self.svg.select("title.tooltip")
            .remove()

          self.svg.select("line.mouse-position")
            .remove()

          self.svgInner
            .select(".points")
            .selectAll("circle")
            .attr("r", 4)
            .classed("opacity-100", false)
        })
    }

    objKeys(this.config.series).forEach(seriesKey => {
      const seriesConfig = this.config.series[seriesKey]!;

      if (seriesConfig.areaColor) {
        this.svgInner
          .append('path')
          .attr("fill", "currentColor")
          .attr("class", `area-${String(seriesKey)} ${(seriesConfig.areaColor || '')}`)
      }

      this.svgInner
        .append('g')
        .append('path')
        .style('fill', 'none')
        .style('stroke', 'currentColor')
        .style('stroke-width', '1')
        .attr('class', `line-${String(seriesKey)} ${seriesConfig.lineColor}`)
    })

    this.svgInner.append("g")
      .attr("class", "vertical-marker")

    this.svgInner.append("g")
      .attr("class", "points")

    if (this.showAxis) {
      this.yAxis = this.svgInner
        .append('g')
        .attr('id', 'y-axis')
        .attr('class', 'text-secondary text-opacity-75 ')
        .style('transform', 'translate(' + (this.width - this.yMargin) + 'px,  0)');

      this.xAxis = this.svgInner
        .append('g')
        .attr('id', 'x-axis')
        .attr('class', 'text-secondary text-opacity-50 ')
        .style('transform', 'translate(0, ' + (this.height - this.yMargin) + 'px)');
    }
  }

  private getTimeRange(): { from: Date, to: Date } {
    const time = {
      from: this.data[0]?.timestamp || 0,
      to: this.data[this.data.length - 1]?.timestamp || 0,
    };

    if (!!this.config.time) {
      time.from = coerceDate(this.config.time.from).getTime() / 1000

      if (this.config.fromMargin) {
        time.from = time.from - this.config.fromMargin
      }

      if (this.config.time.to) {
        time.to = coerceDate(this.config.time.to).getTime() / 1000

        if (this.config.toMargin) {
          time.to = time.to + this.config.toMargin
        }
      }
    }

    return {
      from: new Date(time.from * 1000),
      to: new Date(time.to * 1000)
    };
  }

  private prepareDataSet(data: D[], time: { from: Date, to: Date }) {
    const toTimestamp = Math.round(time.to.getTime() / 1000)
    const fromTimestamp = Math.round(time.from.getTime() / 1000)

    // first, filter out all elements that are before or after the to date
    data = data.filter(d => {
      return d.timestamp >= fromTimestamp && d.timestamp <= toTimestamp
    })

    // check if we need to fill empty ticks
    if (!this.config.fillEmptyTicks) {
      return data;
    }

    const interval = this.config.fillEmptyTicks.interval;

    const filledData: D[] = [];
    const addEmpty = (ts: number) => {
      const empty: any = {
        timestamp: ts,
      }

      Object.keys(this.config.series)
        .forEach(s => empty[s] = 0)

      filledData.push(empty)
    }

    if (!data.length) {
      return [];
    }

    let firstElement = data[0].timestamp;
    if (this.config.time?.from) {
      firstElement = Math.round(coerceDate(this.config.time.from).getTime() / 1000)
    }

    // add empty values for the start-time until the first element / or the start tme
    let lastTimeStamp = fromTimestamp - interval;
    for (let ts = lastTimeStamp; ts <= firstElement; ts += interval) {
      addEmpty(ts)
    }

    // add emepty vaues for each missing tick during the dataset
    lastTimeStamp = firstElement;
    for (let idx = 0; idx < data.length; idx++) {
      const elem = data[idx]
      const elemTs = elem.timestamp;

      for (let ts = lastTimeStamp + interval; ts < elemTs; ts += interval) {
        addEmpty(ts)
      }

      filledData.push(elem)
      lastTimeStamp = elemTs
    }

    // if there's a specified end-time, add empty ticks from the last datapoint
    // to the end-time
    if (this.config.time?.to) {
      for (let ts = lastTimeStamp + interval; ts <= toTimestamp; ts += interval) {
        addEmpty(ts)
      }
    }

    return filledData
  }

  private drawChart(): void {
    if (!this.svg) {
      return;
    }

    if (!this.data?.length) {
      return;
    }

    this.data.sort((a, b) => a.timestamp - b.timestamp)

    // determine the time range that should be displayed.
    const time = this.getTimeRange();

    // fill empty ticks depending on the configuration.
    this.preparedData = this.prepareDataSet(this.data, time)

    this.xScale
      .range([0, this.width - this.yMargin])
      .domain([time.from, time.to]);

    this.yScale
      .range([0, this.height - this.yMargin])
      .domain([
        d3.max(this.preparedData.map(d => {
          return d3.max(
            objKeys(this.config.series)
              .map(series => {
                return d[series] as number
              })
          )!
        }))! * 1.3,  // 30% margin to top
        0
      ])

    if (this.showAxis) {
      const xAxis = d3
        .axisBottom(this.xScale)
        .ticks(5)
        .tickFormat((val, idx) => {
          if (!!this.config.timeFormat) {
            return this.config.timeFormat(val as any)
          }
          return timeAgo(val as any);
        })

      this.xAxis.call(xAxis);

      const yAxis = d3
        .axisLeft(this.yScale)
        .ticks(2)
        .tickFormat(d => ((this.config.valueFormat || this.yScale.tickFormat(2)) as any)(d, undefined))

      this.yAxis.call(yAxis);
    }

    const line = d3
      .line()
      .x(d => d[0])
      .y(d => d[1])
      .curve(d3.curveMonotoneX);

    // define the area
    const area = d3.area()
      .x(d => d[0])
      .y0(this.height - this.yMargin)
      .y1(d => d[1])
      .curve(d3.curveMonotoneX)

    // render vertical markers
    const markers = (this.config.verticalMarkers || [])
      .filter(marker => !!marker.time)
      .map(marker => ({
        text: marker.text,
        time: coerceDate(marker.time)
      }));

    this.svgInner.select('.vertical-marker')
      .selectAll("line.marker")
      .data(markers)
      .join("line")
      .classed("marker", true)
      .attr("x1", d => this.xScale(d.time))
      .attr("y1", -10)
      .attr("x2", d => this.xScale(d.time))
      .attr("y2", this.height - this.yMargin)
      .classed("text-secondary text-opacity-50", true)
      .attr("stroke", "currentColor")
      .attr("stroke-width", 3)
      .attr("stroke-dasharray", 4)
      .append("title")
      .text(d => d.text)

    // FIXME(ppacher): somehow d3 does not recognize which data points must be removed
    // or re-placed. For now, just remove them all
    this.svgInner
      .select('.points')
      .selectAll("circle")
      .remove()

    objKeys(this.config.series)
      .forEach(seriesKey => {
        const config = this.config.series[seriesKey]!;

        let points: [number, number][] = this.preparedData
          .map(d => [
            this.xScale(new Date(d.timestamp * 1000)),
            this.yScale((d as any)[seriesKey] || 0),
          ])

        let data: [number, number][] = this.preparedData
          .map(d => [
            this.xScale(new Date(d.timestamp * 1000)),
            this.yScale((d as any)[seriesKey] || 0),
          ])

        if (config.areaColor) {
          this.svgInner.selectAll(`.area-${String(seriesKey)}`)
            .data([data])
            .attr('d', area(data))
        }

        this.svgInner.select(`.line-${String(seriesKey)}`)
          .attr('d', line(data))

        if (this.config?.showDataPoints) {
          this.svgInner
            .select('.points')
            .selectAll(`circle.point-${String(seriesKey)}`)
            .data(points)
            .enter()
            .append("circle")
            .classed(`points-${String(seriesKey)}`, true)
            .attr("r", "4")
            .attr("fill", "currentColor")
            .attr("class", `opacity-0 ${config.lineColor}`)
            .attr("cx", d => d[0])
            .attr("cy", d => d[1])
            .append("title")
            .text(d => ((this.config.valueFormat || this.yScale.tickFormat(2)) as any)(this.yScale.invert(d[1]), String(seriesKey)))
        }
      })
  }
}
