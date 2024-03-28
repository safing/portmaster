import { AfterViewInit, ChangeDetectionStrategy, Component, DestroyRef, ElementRef, Input, OnInit, inject } from '@angular/core';
import { QueryResult } from '@safing/portmaster-api';
import * as d3 from 'd3';

export interface CircularBarChartConfig<T> {
  // stack either holds the attribute name or an accessor function
  // to determine which serieses belong to the same stack.
  stack: keyof T | ((d: T) => string);

  // series either holds the attribute name of the key or an accessor function.
  seriesKey: keyof T | ((d: T) => string);

  seriesLabel?: (s: string) => string;

  // value either holds the attribute name or an accessor function
  // to get the value of the series.
  value: keyof T | ((d: T) => number);

  colorAsClass?: boolean;

  // the actual series configuration
  series?: {
    [key: string]: {
      color: string;
    }
  };

  // The number of ticks for the y axis
  ticks?: number;

  formatTick?: (v: number) => string;

  // an optional function to format the value
  formatValue?: (stack: string, series: string, value: number, data?: T) => string;

  formatStack?: (sel: d3.Selection<SVGGElement, string, SVGGElement, any>, data: T[]) => d3.Selection<any, any, any, any>;
}


export function splitQueryResult<T extends QueryResult, K extends keyof T>(results: T[], series: K[]): (QueryResult & { series: string, value: number })[] {
  let mapped: (QueryResult & { series: string, value: number })[] = [];

  results.forEach(row => {
    series.forEach(seriesKey => {
      mapped.push({
        ...row,
        value: row[seriesKey],
        series: seriesKey as string,
      })
    })
  })

  return mapped
}

@Component({
  selector: 'sfng-netquery-circular-bar-chart',
  template: '',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class CircularBarChartComponent<T> implements OnInit, AfterViewInit {
  private readonly elementRef = inject(ElementRef) as ElementRef<HTMLElement>;
  private readonly destroyRef = inject(DestroyRef);

  // D3 related members
  private svg?: d3.Selection<SVGGElement, any, any, any>;
  private x?: d3.ScaleBand<string>;
  private y?: d3.ScaleRadial<any, any>;
  private height = 0;
  private width = 0;

  @Input()
  config: CircularBarChartConfig<T> | null = null;

  @Input()
  innerRadius?: number;

  @Input()
  set data(d: T[] | null) {
    this._data = d || [];

    this.prepareChart()
    this.render();
  }
  private _data: T[] = [];

  ngOnInit(): void {
    this.prepareChart()
    this.render()
  }

  ngAfterViewInit(): void {
    const observer = new ResizeObserver(() => {
      this.prepareChart()
      this.render()
    })

    observer.observe(this.elementRef.nativeElement)

    this.destroyRef.onDestroy(() => observer.disconnect())

    this.prepareChart()
    this.render();
  }

  private prepareChart() {
    if (!!this.svg) {
      const parent = this.svg.node()?.parentElement
      parent?.remove()
    }

    const margin = 0.2
    const bbox = this.elementRef.nativeElement.getBoundingClientRect();

    const marginLeft = bbox.width * margin;
    const marginTop = bbox.height * margin;
    this.width = bbox.width - 2 * marginLeft;
    this.height = bbox.height - 2 * marginTop;

    this.svg = d3.select(this.elementRef.nativeElement)
      .append('svg')
      .attr('width', "100%")
      .attr('height', "100%")
      .append('g')
      .attr('transform', `translate(${this.width / 2 + marginLeft}, ${this.height / 2 + marginTop})`);


    this.x = d3.scaleBand()
      .range([0, 2 * Math.PI])
      .align(0);

    this.y = d3.scaleRadial()

    // prepare the SVGGElement that we use for rendering
    this.svg.append("g")
      .attr("id", "chart")

    this.svg.append("g")
      .attr("id", "text")

    this.svg.append("g")
      .attr("id", "legend")

    this.svg.append("g")
      .attr("id", "ticks")
  }

  private render() {
    const x = this.x;
    const y = this.y;

    if (!this.svg || !x || !y) {
      console.log("not yet ready")
      return;
    }

    let stackName: (d: T) => string;
    if (typeof this.config?.stack === 'function') {
      stackName = this.config.stack;
    } else {
      stackName = (d: T) => {
        return d[this.config!.stack as keyof T] + ''
      }
    }

    let seriesKey: (d: T) => string;
    if (typeof this.config?.seriesKey === 'function') {
      seriesKey = this.config!.seriesKey
    } else {
      seriesKey = (d: T) => {
        return d[this.config!.seriesKey as keyof T] + ''
      }
    }

    let value: (d: T) => number;
    if (typeof this.config?.value === 'function') {
      value = this.config!.value
    } else {
      value = (d: T) => {
        return +d[this.config!.value as keyof T]
      }
    }

    let formatValue: Exclude<CircularBarChartConfig<T>["formatValue"], undefined> = (stack, series, value) => `${stack} ${series}\n${value}`
    if (this.config?.formatValue) {
      formatValue = this.config.formatValue;
    }

    // Prepare the stacked data
    const indexed = d3.index(this._data, stackName, seriesKey)
    const stackGenerator = d3.stack<[string, d3.InternMap<string, T>]>()
      .keys(d3.union(this._data.map(seriesKey)))
      .value((data, key) => {
        const obj = data[1].get(key)
        if (obj === undefined) {
          return 0
        }

        return value(obj);
      })

    const series = stackGenerator(indexed)

    // Prepare the x domain
    const labels = new Set<string>();
    this._data.forEach(d => labels.add(stackName(d)));
    this.x!.domain(Array.from(labels))
      .range([0, 2 * Math.PI])
      .align(0);

    const innerRadius = this.innerRadius || (() => {
      return (series.length * 25) + 20
    })()

    // Prepare the x domain
    const outerRadius = Math.min(this.width, this.height) / 2;
    const highest = d3.max(series, point => d3.max(point, point => point[1])!)!
    this.y!.domain([0, highest])
      .range([innerRadius, outerRadius]);


    const arc = d3.arc()
      .innerRadius((d: any) => y(d[0]))
      .outerRadius((d: any) => y(d[1]))
      .startAngle((d: any) => x(d.data[0])!)
      .endAngle((d: any) => x(d.data[0])! + x.bandwidth())
      .padAngle(0.01)
      .padRadius(innerRadius)

    let color: (key: string) => string;

    if (!this.config?.series) {
      const colorScale: d3.ScaleOrdinal<string, any, any> = d3.scaleOrdinal()
        .domain(series.map(d => d.key))
        .range(d3.schemeSpectral)
        .unknown("#ccc")

      color = key => colorScale(key);
    } else {
      color = key => this.config!.series![key].color
    }

    this.svg.select("g#chart")
      .selectAll()
      .data(series)
      .join("g")
      .call(g => {
        if (this.config?.colorAsClass) {
          g.attr("fill", "currentColor")
            .attr("class", d => color(d.key))
        } else {
          g.attr("fill", d => color(d.key))
        }
      })
      .selectAll("path")
      .data(D => D.map(d => ((d as any).key = D.key, d)))
      .join("path")
      .attr("d", arc as any)
      .append("title")
      .text(d => {
        const stack = d.data[0]
        const series = (d as any).key
        const data = d.data[1].get(series);
        const seriesValue = data ? value(data) : 0;

        return formatValue(stack, series, seriesValue, data);
      })

    const sumPerLabel = this._data.reduce((map, current) => {
      const stack = stackName(current)
      let sum = map.get(stack) || 0
      sum += value(current)
      map.set(stack, sum)

      return map
    }, new Map<string, number>());

    this.svg.select("g#text")
      .attr("text-anchor", "middle")
      .selectAll()
      .data(x.domain())
      .join("g")
      .attr("text-anchor", d => (x(d)! + x.bandwidth() / 2 + Math.PI) % (2 * Math.PI) < Math.PI ? "end" : "start")
      .attr("transform", d => "rotate(" + ((x(d)! + this.x!.bandwidth() / 2) * 180 / Math.PI - 90) + ")" + "translate(" + (y(sumPerLabel.get(d)!) + 10) + ",0)")
      .append("g")
      .attr("transform", d => (x(d)! + x.bandwidth() / 2 + Math.PI) % (2 * Math.PI) < Math.PI ? "rotate(180)" : "rotate(0)")
      .style("font-size", "11px")
      .attr("alignment-baseline", "middle")
      .attr("fill", "currentColor")
      .attr("class", "text-primary cursor-pointer")
      .on("mouseenter", function (data) {
        d3.select(this)
          .classed("underline", true)
      })
      .on("mouseleave", function (data) {
        d3.select(this)
          .classed("underline", false)
      })
      .call(g => {
        if (!this.config?.formatStack) {
          return g.append("text")
            .text(d => `${d}`)
        }

        return this.config.formatStack(g as any, this._data)
      })

    // y axis
    const tickCount = this.config?.ticks || Math.floor((outerRadius - innerRadius) / 20)
    const tickFormat = this.config?.formatTick || y.tickFormat(tickCount, "s")
    this.svg.select("g#ticks")
      .attr("text-anchor", "middle")
      .selectAll("g")
      .data(y.ticks(tickCount).slice(1))
      .join("g")
      .attr("fill", "none")
      .call(g => g.append("circle")
        .attr("stroke", "#fff")
        .attr("stroke-opacity", 0.25)
        .attr("r", y))
      .call(g => g.append("text")
        .style("font-size", "0.6rem")
        .attr("y", d => -y(d))
        .attr("dy", "0.35em")
        .attr("fill", "currentColor")
        .attr("class", "text-secondary")
        .text(tickFormat))

    // color legend
    this.svg.select("g#legend")
      .selectAll()
      .data(series.map(s => s.key))
      .join("g")
      .attr("transform", (d, i, nodes) => `translate(-40,${(nodes.length / 2 - i - 1) * 20})`)
      .call(g => g.append("circle")
        .attr("r", 5)
        .call(g => {
          if (this.config?.colorAsClass) {
            g.attr("fill", "currentColor")
              .attr("class", d => color(d))
          } else {
            g.attr("fill", d => color(d))
          }
        }))
      .call(g => g.append("text")
        .attr("x", 12)
        .attr("y", 4)
        .attr("font-size", "0.6rem")
        .attr("fill", "#fff")
        .text(d => {
          if (!!this.config?.seriesLabel) {
            return this.config.seriesLabel(d)
          }

          return d
        }));
  }
}
