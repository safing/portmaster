import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, Input, OnInit, inject } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { AppProfile, BandwidthChartResult, ChartResult, Netquery } from '@safing/portmaster-api';
import { repeat } from 'rxjs';
import { CircularBarChartConfig, splitQueryResult } from 'src/app/shared/netquery/circular-bar-chart/circular-bar-chart.component';
import { DefaultBandwidthChartConfig } from 'src/app/shared/netquery/line-chart/line-chart';

interface CountryBarData {
  series: 'country';
  value: number;
  country: string;
}

@Component({
  selector: 'app-app-insights',
  templateUrl: './app-insights.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class AppInsightsComponent implements OnInit {
  private readonly netquery = inject(Netquery);
  private readonly destroyRef = inject(DestroyRef);
  private readonly cdr = inject(ChangeDetectorRef);

  @Input()
  profile!: AppProfile;

  connectionChart: ChartResult[] = [];

  bandwidthChart: BandwidthChartResult<any>[] = [];

  bwChartConfig = DefaultBandwidthChartConfig;

  countryData: CountryBarData[] = [];

  readonly countryBarConfig: CircularBarChartConfig<CountryBarData> = {
    stack: 'country',
    seriesKey: 'series',
    value: 'value',
    ticks: 3,
    colorAsClass: true,
    series: {
      'count': {
        color: 'text-green-300 text-opacity-50',
      },
    },
  }

  ngOnInit() {
    const key = `${this.profile.Source}/${this.profile.ID}`

    this.netquery.batch({
      countryData: {
        select: [
          'country',
          { $count: { field: '*', as: 'count' } },
        ],
        query: {
          internal: { $eq: false },
          country: { $ne: '' }
        },
        groupBy: ['country']
      }
    })
      .pipe(
        repeat({ delay: 10000 }),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe(result => {
        this.countryData = splitQueryResult(result.countryData, ['count']) as CountryBarData[];
        console.log(this.countryData)
        this.cdr.markForCheck();
      })

    this.netquery.activeConnectionChart({ profile: key })
      .pipe(
        repeat({ delay: 10000 }),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe(data => {
        this.connectionChart = data;
        this.cdr.markForCheck();
      })

    this.netquery.bandwidthChart({ profile: key }, undefined, 60)
      .pipe(
        repeat({ delay: 10000 }),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe(data => {
        this.bandwidthChart = data;
        this.cdr.markForCheck();
      })

  }

}
