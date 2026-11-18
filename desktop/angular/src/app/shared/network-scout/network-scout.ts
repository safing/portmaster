import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, OnInit, TrackByFunction, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { Condition, ExpertiseLevel, IProfileStats, Netquery } from "@safing/portmaster-api";
import { Subject, combineLatest, debounceTime, filter, finalize, interval, retry, startWith, switchMap, take } from "rxjs";
import { UIStateService } from "src/app/services";
import { fadeInListAnimation } from "../animations";
import { ExpertiseService } from './../expertise/expertise.service';

type _Profile = IProfileStats;

export enum SortTypes {
  static = 'Static',
  aToZ = "A-Z",
  zToA = "Z-A",
  totalConnections = "Total Connections",
  connectionsDenied = "Denied Connections",
  connectionsAllowed = "Allowed Connections",
  bytesSent = "Bytes Sent",
  bytesReceived = "Bytes Received",
  totalBytes = "Total Bytes"
}

const bandwidthSorts: SortTypes[] = [
  SortTypes.bytesReceived,
  SortTypes.bytesSent,
  SortTypes.totalBytes
]

@Component({
  selector: 'app-network-scout',
  templateUrl: './network-scout.html',
  styleUrls: ['./network-scout.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    fadeInListAnimation,
  ]
})
export class NetworkScoutComponent implements OnInit {
  private destroyRef = inject(DestroyRef);

  sortTypes = [
    SortTypes.static,
    SortTypes.aToZ,
    SortTypes.zToA,
    SortTypes.totalConnections,
    SortTypes.connectionsDenied,
    SortTypes.connectionsAllowed,
  ]

  readonly sortMethods = new Map<SortTypes, any>([
    // there's not entry for "Static" here on purpose because we'll use the sort order
    // returned by netquery.
    [SortTypes.aToZ, (a: _Profile, b: _Profile) => a.Name.localeCompare(b.Name)],
    [SortTypes.zToA, (a: _Profile, b: _Profile) => b.Name.localeCompare(a.Name)],
    [SortTypes.totalConnections, (a: _Profile, b: _Profile) => (b.countAllowed + b.countUnpermitted) - (a.countAllowed + a.countUnpermitted)],
    [SortTypes.connectionsAllowed, (a: _Profile, b: _Profile) => b.countAllowed - a.countAllowed],
    [SortTypes.connectionsDenied, (a: _Profile, b: _Profile) => b.countUnpermitted - a.countUnpermitted],
    [SortTypes.bytesReceived, (a: _Profile, b: _Profile) => b.bytes_received - a.bytes_received],
    [SortTypes.bytesSent, (a: _Profile, b: _Profile) => b.bytes_sent - a.bytes_sent],
    [SortTypes.totalBytes, (a: _Profile, b: _Profile) => (b.bytes_received + b.bytes_sent) - (a.bytes_received + a.bytes_sent)]
  ]);

  /** The current sort order */
  sortOrder: SortTypes = SortTypes.static;

  get isByteSortOrder() {
    return bandwidthSorts.includes(this.sortOrder);
  }

  /** Used to trigger a debounced search from the template */
  triggerSearch = new Subject<string>();

  /** The current search term as entered in the input[type="text"] */
  searchTerm: string = '';

  /** A list of all active profiles without any search applied */
  allProfiles: _Profile[] = [];

  /**
   * A list of profiles that are currently displayed. This is basically allProfiles but with
   * text search applied.
   */
  profiles: _Profile[] = [];

  /** TrackByFunction for the profiles. */
  trackProfile: TrackByFunction<_Profile> = (_, profile) => profile.ID;

  constructor(
    private netquery: Netquery,
    private stateService: UIStateService,
    private expertise: ExpertiseService,
    private cdr: ChangeDetectorRef,
  ) { }

  searchProfiles(term: string) {
    term = term.trim();

    if (term === '') {
      this.profiles = [
        ...this.allProfiles
      ];

      this.sortProfiles(this.profiles);

      return;
    }

    const lowerCaseTerm = term.toLocaleLowerCase()
    this.profiles = this.allProfiles.filter(p => {
      if (p.ID.toLocaleLowerCase().includes(lowerCaseTerm)) {
        return true;
      }

      if (p.Name.toLocaleLowerCase().includes(lowerCaseTerm)) {
        return true;
      }

      return false;
    })

    this.sortProfiles(this.profiles);
  }

  sortProfiles(profiles: _Profile[]) {
    const method = this.sortMethods.get(this.sortOrder);
    if (!method) {
      return;
    }

    profiles.sort(method)

    this.cdr.markForCheck();
  }

  updateSortOrder(newOrder: SortTypes) {
    this.sortOrder = newOrder;
    this.searchProfiles(this.searchTerm);

    this.stateService.set('netscoutSortOrder', newOrder)
      .subscribe({
        error: err => {
          console.error(err);
        }
      })
  }

  ngOnInit(): void {
    this.stateService.uiState()
      .pipe(take(1))
      .subscribe(state => {
        // fall back to the default if the stored sort order is no longer
        // available (e.g. the removed "SPN Identities" order).
        this.sortOrder = Object.values(SortTypes).includes(state.netscoutSortOrder)
          ? state.netscoutSortOrder
          : SortTypes.static;

        this.searchProfiles(this.searchTerm);
      })

    let updateInProgress = false;

    combineLatest([
      combineLatest([
        interval(5000)
          .pipe(
            filter(() => !updateInProgress)
          ),
        this.expertise.change,
      ])
        .pipe(
          startWith(-1),
          switchMap(() => {
            let query: Condition = {};
            if (this.expertise.currentLevel !== ExpertiseLevel.Developer) {
              query["internal"] = { $eq: false }
            }

            updateInProgress = true

            return this.netquery.getProfileStats(query)
              .pipe(
                finalize(() => updateInProgress = false)
              )
          }),
          retry({ delay: 5000 })
        ),

      this.triggerSearch
        .pipe(
          debounceTime(100),
          startWith(''),
        ),
    ])
      .pipe(
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(([res, searchTerm]) => {
        this.allProfiles = res;

        this.searchProfiles(searchTerm);

        // check if we have profiles with bandwidth data and
        // make sure our sort methods are updated.
        if (this.profiles.some(p => p.bytes_received > 0 || p.bytes_sent > 0)) {
          if (!this.sortTypes.includes(SortTypes.bytesReceived)) {
            this.sortTypes.push.apply(this.sortTypes, bandwidthSorts)
          }

          this.sortTypes = [...this.sortTypes];
        } else {
          this.sortTypes = this.sortTypes.filter(type => {
            return !bandwidthSorts.includes(type)
          })
        }

        this.cdr.markForCheck();
      })
  }
}
