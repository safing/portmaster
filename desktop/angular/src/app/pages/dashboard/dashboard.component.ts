import { KeyValue } from "@angular/common";
import { AfterContentInit, AfterViewInit, ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, ElementRef, OnInit, QueryList, TrackByFunction, ViewChild, ViewChildren, forwardRef, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { AppProfileService, BandwidthChartResult, ChartResult, Database, FeatureID, Netquery, PortapiService, SPNService, UserProfile, Verdict } from "@safing/portmaster-api";
import { SfngDialogService, SfngTabGroupComponent } from "@safing/ui";
import { Observable, catchError, filter, interval, map, repeat, retry, startWith, throwError } from "rxjs";
import { ActionIndicatorService } from 'src/app/shared/action-indicator';
import { DefaultBandwidthChartConfig, SfngNetqueryLineChartComponent } from "src/app/shared/netquery/line-chart/line-chart";
import { SPNAccountDetailsComponent } from "src/app/shared/spn-account-details";
import { MAP_HANDLER, MapRef } from "../spn/map-renderer";
import { CircularBarChartConfig, splitQueryResult } from "src/app/shared/netquery/circular-bar-chart/circular-bar-chart.component";
import { BytesPipe } from "src/app/shared/pipes/bytes.pipe";
import { HttpErrorResponse } from "@angular/common/http";

interface BlockedProfile {
  profileID: string;
  count: number;
}

interface BandwidthBarData {
  profile: string;
  profile_name: string;
  series: 'sent' | 'received';
  value: number;
  sent: number;
  received: number;
}

interface NewsCard {
  title: string;
  body: string;
  url?: string;
  footer?: string;
  progress?: {
    percent: number;
    style: string;
  }
}

interface News {
  cards: NewsCard[];
}

const newsResourceIdentifier = "all/intel/portmaster/news.yaml"

@Component({
  selector: 'app-dashboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrls: ['./dashboard.component.scss'],
  templateUrl: './dashboard.component.html',
  providers: [
    { provide: MAP_HANDLER, useExisting: forwardRef(() => DashboardPageComponent), multi: true },
  ]
})
export class DashboardPageComponent implements OnInit, AfterViewInit {
  @ViewChildren(SfngNetqueryLineChartComponent)
  lineCharts!: QueryList<SfngNetqueryLineChartComponent>;

  @ViewChild(SfngTabGroupComponent)
  carouselTabGroup?: SfngTabGroupComponent;

  private readonly destroyRef = inject(DestroyRef);
  private readonly netquery = inject(Netquery);
  private readonly spn = inject(SPNService);
  private readonly actionIndicator = inject(ActionIndicatorService);
  private readonly cdr = inject(ChangeDetectorRef);
  private readonly dialog = inject(SfngDialogService);
  private readonly portapi = inject(PortapiService)

  resizeObserver!: ResizeObserver;

  blockedProfiles: BlockedProfile[] = []

  connectionsPerCountry: {
    [country: string]: number
  } = {};

  get countryNames(): { [country: string]: string } {
    return this.mapRef?.countryNames || {};
  }

  bandwidthLineChart: BandwidthChartResult<any>[] = [];

  bandwidthBarData: BandwidthBarData[] = [];

  readonly bandwidthBarConfig: CircularBarChartConfig<BandwidthBarData> = {
    stack: 'profile_name',
    seriesKey: 'series',
    seriesLabel: d => {
      if (d === 'sent') {
        return 'Bytes Sent'
      }
      return 'Bytes Received'
    },
    value: 'value',
    ticks: 3,
    colorAsClass: true,
    series: {
      'sent': {
        color: 'text-deepPurple-500 text-opacity-50',
      },
      'received': {
        color: 'text-cyan-800 text-opacity-50',
      }
    },
    formatTick: (tick: number) => {
      return new BytesPipe().transform(tick, '1.0-0')
    },
    formatValue: (stack, series, value, data) => {
      const bytes = new BytesPipe().transform
      return `${stack}\nSent: ${bytes(data?.sent)}\nReceived: ${bytes(data?.received)}`
    },
    formatStack: (sel, data) => {
      const bytes = new BytesPipe().transform

      return sel
        .call(sel => {
          sel.append("text")
            .attr("dy", "0")
            .attr("y", "0")
            .text(d => d)
        })
        .call(sel => {
          sel.append("text")
            .attr("y", 0)
            .attr("dy", "0.8rem")
            .style("font-size", "0.6rem")
            .text(d => {
              const first = data.find(result => result.profile_name === d);
              return `${bytes(first?.sent)} / ${bytes(first?.received)}`
            })
        })
    }
  }

  bwChartConfig = DefaultBandwidthChartConfig;

  activeConnections: number = 0;
  blockedConnections: number = 0;
  activeProfiles: number = 0;
  activeIdentities = 0;
  dataIncoming = 0;
  dataOutgoing = 0;
  connectionChart: ChartResult[] = [];
  tunneldConnectionChart: ChartResult[] = [];

  countriesPerProfile: { [profile: string]: string[] } = {}

  profile: UserProfile | null = null;

  featureBw = false;
  featureSPN = false;

  hoveredCard: NewsCard | null = null;

  features$ = this.spn.watchEnabledFeatures()
    .pipe(takeUntilDestroyed());

  trackCountry: TrackByFunction<KeyValue<string, any>> = (_, ctr) => ctr.key;
  trackApp: TrackByFunction<BlockedProfile> = (_, bp) => bp.profileID;

  data: any;

  news?: News | 'pending' = 'pending';

  private mapRef: MapRef | null = null;

  registerMap(ref: MapRef): void {
    this.mapRef = ref;

    this.mapRef.onMapReady(() => {
      this.updateMapCountries();
    })
  }

  private updateMapCountries() {
    // this check is basically to make typescript happy ...
    if (!this.mapRef) {
      return;
    }

    this.mapRef.worldGroup
      .selectAll('path')
      .classed('active', (d: any) => {
        return !!this.connectionsPerCountry[d.properties.iso_a2];
      });
  }

  unregisterMap(ref: MapRef): void {
    this.mapRef = null;
  }

  onCarouselTabHover(card: NewsCard | null) {
    this.hoveredCard = card;
  }

  openAccountDetails() {
    this.dialog.create(SPNAccountDetailsComponent, {
      autoclose: true,
      backdrop: 'light'
    })
  }

  onCountryHover(code: string | null) {
    if (!this.mapRef) {
      return
    }

    this.mapRef.worldGroup
      .selectAll('path')
      .classed('hover', (d: any) => {
        return (d.properties.iso_a2 === code);
      });
  }

  onProfileHover(profile: string | null) {
    if (!this.mapRef) {
      return
    }

    this.mapRef.worldGroup
      .selectAll('path')
      .classed('hover', (d: any) => {
        if (!profile) {
          return false;
        }

        return this.countriesPerProfile[profile]?.includes(d.properties.iso_a2);
      });
  }

  ngAfterViewInit(): void {
    interval(15000)
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        startWith(-1),
        filter(() => this.hoveredCard === null)
      )
      .subscribe(() => {
        if (!this.carouselTabGroup) {
          return
        }

        let next = this.carouselTabGroup.activeTabIndex + 1
        if (next >= this.carouselTabGroup.tabs!.length) {
          next = 0
        }

        this.carouselTabGroup.activateTab(next, "left")
      })
  }

  async ngOnInit() {
    this.portapi.getResource<News>(newsResourceIdentifier)
      .pipe(
        repeat({ delay: 60000 }),
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe({
        next: response => {
          this.news = response;
          this.cdr.markForCheck();
        },
        error: () => {
          this.news = undefined;
          this.cdr.markForCheck();
        }
      });

    this.netquery
      .batch({
        bwBarChart: {
          query: {
            internal: { $eq: false },
          },
          select: [
            'profile',
            'profile_name',
            {
              $sum: {
                field: 'bytes_sent',
                as: 'sent'
              }
            },
            {
              $sum: {
                field: 'bytes_received',
                as: 'received'
              }
            },
          ],
          groupBy: ['profile', 'profile_name'],
        },

        profileCount: {
          select: [
            'profile',
            {
              $count: {
                field: '*',
                as: 'totalCount'
              }
            }
          ],
          query: {
            verdict: { $in: [Verdict.Block, Verdict.Drop] }
          },
          groupBy: ['profile'],
          databases: [Database.Live]
        },

        countryStats: {
          select: [
            'country',
            { $count: { field: '*', as: 'totalCount' } },
            { $sum: { field: 'bytes_sent', as: 'bwout' } },
            { $sum: { field: 'bytes_received', as: 'bwin' } },
          ],
          query: {
            allowed: { $eq: true },
          },
          groupBy: ['country'],
          databases: [Database.Live]
        },

        perCountryConns: {
          select: ['profile', 'country', 'active', { $count: { field: '*', as: 'totalCount' } }],
          query: {
            allowed: { $eq: true },
          },
          groupBy: ['profile', 'country', 'active'],
          databases: [Database.Live],
        },

        exitNodes: {
          query: { tunneled: { $eq: true }, exit_node: { $ne: "" } },
          groupBy: ['exit_node'],
          select: [
            'exit_node',
            { $count: { field: '*', as: 'totalCount' } }
          ],
          databases: [Database.Live],
        }
      })
      .pipe(
        repeat({ delay: 10000 }),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(response => {
        // bandwidth bar chart
        const barChartData = response.bwBarChart
          .filter(value => (value.sent + value.received) > 0)
          .sort((a, b) => (b.sent + b.received) - (a.sent + a.received))
          .slice(0, 10);
        this.bandwidthBarData = splitQueryResult(barChartData, ['sent', 'received']) as BandwidthBarData[]

        // profileCount
        this.blockedConnections = 0;
        this.blockedProfiles = [];

        response.profileCount?.forEach(row => {
          this.blockedConnections += row.totalCount;
          this.blockedProfiles.push({
            profileID: row.profile!,
            count: row.totalCount
          })
        });

        // countryStats
        this.connectionsPerCountry = {};
        this.dataIncoming = 0;
        this.dataOutgoing = 0;

        response.countryStats?.forEach(row => {
          this.dataIncoming += row.bwin;
          this.dataOutgoing += row.bwout;

          if (row.country === '') {
            return
          }

          this.connectionsPerCountry[row.country!] = row.totalCount || 0;
        })

        this.updateMapCountries()

        // perCountryConns
        let profiles = new Set<string>();

        this.activeConnections = 0;
        this.countriesPerProfile = {};

        response.perCountryConns?.forEach(row => {
          profiles.add(row.profile!);

          if (row.active) {
            this.activeConnections += row.totalCount;
          }

          const arr = (this.countriesPerProfile[row.profile!] || []);
          arr.push(row.country!)
          this.countriesPerProfile[row.profile!] = arr;
        });

        this.activeProfiles = profiles.size;

        // exitNodes
        this.activeIdentities = response.exitNodes?.length || 0;
        this.cdr.markForCheck();
      })


    // Charts

    this.netquery
      .activeConnectionChart({})
      .pipe(
        repeat({ delay: 10000 }),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(result => {
        this.connectionChart = result;
        this.cdr.markForCheck();
      })

    this.netquery
      .bandwidthChart({}, undefined, 60)
      .pipe(
        repeat({ delay: 10000 }),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(bw => {
        this.bandwidthLineChart = bw;
        this.cdr.markForCheck();
      })

    this.netquery
      .activeConnectionChart({ tunneled: { $eq: true } })
      .pipe(
        repeat({ delay: 10000 }),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(result => {
        this.tunneldConnectionChart = result;
        this.cdr.markForCheck();
      })

    // SPN profile and enabled/allowed features

    this.spn
      .profile$
      .pipe(
        takeUntilDestroyed(this.destroyRef)
      )
      .subscribe({
        next: (profile) => {
          this.profile = profile || null;
          this.featureBw = profile?.current_plan?.feature_ids?.includes(FeatureID.Bandwidth) || false;
          this.featureSPN = profile?.current_plan?.feature_ids?.includes(FeatureID.SPN) || false;

          // force a full change-detection cylce now!
          this.cdr.detectChanges()

          // force re-draw of the charts after change-detection because the
          // width may change now.
          this.lineCharts?.forEach(chart => chart.redraw())

          this.cdr.markForCheck();
        },
      })
  }

  /** Logs the user out of the SPN completely by purgin the user profile from the local storage */
  logoutCompletely(_: Event) {
    this.spn.logout(true)
      .subscribe(this.actionIndicator.httpObserver(
        'Logout',
        'You have been logged out of the SPN completely.'
      ))
  }
}
