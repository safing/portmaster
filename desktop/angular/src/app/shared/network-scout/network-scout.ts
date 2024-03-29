import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, OnInit, TrackByFunction, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { BoolSetting, Condition, ConfigService, ExpertiseLevel, IProfileStats, Netquery, Pin, SPNService } from "@safing/portmaster-api";
import { Subject, combineLatest, debounceTime, filter, finalize, interval, retry, startWith, switchMap, take, takeUntil } from "rxjs";
import { UIStateService } from "src/app/services";
import { fadeInListAnimation } from "../animations";
import { ExpertiseService } from './../expertise/expertise.service';

interface _Pin extends Pin {
  count: number;
}

interface _Profile extends IProfileStats {
  exitPins: _Pin[];
  showMore: boolean;
  expanded: boolean;
}

export enum SortTypes {
  static = 'Static',
  aToZ = "A-Z",
  zToA = "Z-A",
  totalConnections = "Total Connections",
  connectionsDenied = "Denied Connections",
  connectionsAllowed = "Allowed Connections",
  spnIdentities = "SPN Identities",
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
    SortTypes.spnIdentities
  ]

  readonly sortMethods = new Map<SortTypes, any>([
    // there's not entry for "Static" here on purpose because we'll use the sort order
    // returned by netquery.
    [SortTypes.aToZ, (a: _Profile, b: _Profile) => a.Name.localeCompare(b.Name)],
    [SortTypes.zToA, (a: _Profile, b: _Profile) => b.Name.localeCompare(a.Name)],
    [SortTypes.totalConnections, (a: _Profile, b: _Profile) => (b.countAllowed + b.countUnpermitted) - (a.countAllowed + a.countUnpermitted)],
    [SortTypes.connectionsAllowed, (a: _Profile, b: _Profile) => b.countAllowed - a.countAllowed],
    [SortTypes.connectionsDenied, (a: _Profile, b: _Profile) => b.countUnpermitted - a.countUnpermitted],
    [SortTypes.spnIdentities, (a: _Profile, b: _Profile) => a.identities.length - b.identities.length],
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

  /** Defines if new elements should be expanded or collapsed */
  expandCollapseState: 'expand' | 'collapse' = 'expand';

  /** Whether or not the SPN is enabled */
  spnEnabled = false;

  /**
   * Emits when the user clicks the "expand all" or "collapse all" buttons.
   * Once the user did that we stop updating the default state depending on whether the
   * SPN is enabled or not.
   */
  private userChangedState = new Subject<void>();

  /**
   * A list of profiles that are currently displayed. This is basically allProfiles but with
   * text search applied.
   */
  profiles: _Profile[] = [];

  /** TrackByFunction for the profiles. */
  trackProfile: TrackByFunction<_Profile> = (_, profile) => profile.ID;

  /** TrackByFunction for the exit pins */
  trackPin: TrackByFunction<_Pin> = (_, pin) => pin.ID;

  constructor(
    private netquery: Netquery,
    private spn: SPNService,
    private configService: ConfigService,
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

      if (p.exitPins.some(pin => pin.Name.toLocaleLowerCase().includes(lowerCaseTerm))) {
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

  expandAll() {
    this.expandCollapseState = 'expand';
    this.allProfiles.forEach(profile => profile.expanded = profile.identities.length > 0)
    this.searchProfiles(this.searchTerm)
    this.userChangedState.next();

    this.cdr.markForCheck()
  }

  collapseAll() {
    this.expandCollapseState = 'collapse';
    this.allProfiles.forEach(profile => profile.expanded = false)
    this.searchProfiles(this.searchTerm)
    this.userChangedState.next();

    this.cdr.markForCheck()
  }

  ngOnInit(): void {
    this.stateService.uiState()
      .pipe(take(1))
      .subscribe(state => {
        this.sortOrder = state.netscoutSortOrder;

        this.searchProfiles(this.searchTerm);
      })

    this.configService.watch<BoolSetting>('spn/enable')
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        takeUntil(this.userChangedState),
      )
      .subscribe(enabled => {
        // if the SPN is enabled and the user did not yet change the
        // collapse/expand state we switch to "expand" for the default.
        // Otherwise, there will be no identities so there's no reason
        // to expand them at all so we switch to collapse
        if (enabled) {
          this.expandCollapseState = 'expand'
        } else {
          this.expandCollapseState = 'collapse'
        }

        this.spnEnabled = enabled;
      });

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

      this.spn.watchPins()
        .pipe(
          debounceTime(100),
          startWith([]),
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
      .subscribe(([res, pins, searchTerm]) => {
        // create a lookup map for the the SPN map pins
        const pinLookupMap = new Map<string, Pin>();
        pins.forEach(p => pinLookupMap.set(p.ID, p))

        // create a lookup map from already known profiles so we can
        // inherit states like "showMore".
        const profileLookupMap = new Map<string, _Profile>();
        this.allProfiles.forEach(p => profileLookupMap.set(p.ID, p))

        // map the list of profile statistics to include the exit Pin information
        // as well.
        this.allProfiles = res.map(s => {
          const existing = profileLookupMap.get(s.ID);
          return {
            ...s,
            exitPins: s.identities
              .map(ident => {
                const pin = pinLookupMap.get(ident.exit_node);
                if (!pin) {
                  return null;
                }

                return {
                  count: ident.count,
                  ...pin
                }
              })
              .filter(pin => !!pin),
            showMore: existing?.showMore ?? false,
            expanded: existing?.expanded ?? (this.expandCollapseState === 'expand' && s.identities.length > 1 /* there's always the "direct" identity */),
          } as _Profile
        });

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
