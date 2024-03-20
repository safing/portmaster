import {
  ChangeDetectorRef,
  Component,
  OnDestroy,
  OnInit,
  TrackByFunction,
} from '@angular/core';
import {
  AppProfile,
  AppProfileService,
  Netquery,
  trackById,
} from '@safing/portmaster-api';
import { SfngDialogService } from '@safing/ui';
import { BehaviorSubject, Subscription, combineLatest, forkJoin } from 'rxjs';
import { debounceTime, filter, startWith } from 'rxjs/operators';
import {
  fadeInAnimation,
  fadeInListAnimation,
  moveInOutListAnimation,
} from 'src/app/shared/animations';
import { FuzzySearchService } from 'src/app/shared/fuzzySearch';
import { EditProfileDialog } from './../../shared/edit-profile-dialog/edit-profile-dialog';
import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { MergeProfileDialogComponent } from './merge-profile-dialog/merge-profile-dialog.component';
import { ActionIndicatorService } from 'src/app/shared/action-indicator';
import { Router } from '@angular/router';
import {
  ImportConfig,
  ImportDialogComponent,
} from 'src/app/shared/config/import-dialog/import-dialog.component';

interface LocalAppProfile extends AppProfile {
  hasConfigChanges: boolean;
  selected: boolean;
}

@Component({
  selector: 'app-settings-overview',
  templateUrl: './overview.html',
  styleUrls: ['../page.scss', './overview.scss'],
  animations: [fadeInAnimation, fadeInListAnimation, moveInOutListAnimation],
})
export class AppOverviewComponent implements OnInit, OnDestroy {
  private subscription = Subscription.EMPTY;

  /** Whether or not we are currently loading */
  loading = true;

  /** All application profiles that are actually running */
  runningProfiles: LocalAppProfile[] = [];

  /** All application profiles that have been edited recently */
  recentlyEdited: LocalAppProfile[] = [];

  /** All application profiles */
  profiles: LocalAppProfile[] = [];

  /** The current search term */
  searchTerm: string = '';

  /** total number of profiles */
  total: number = 0;

  /** Whether or not we are in profile-selection mode */
  set selectMode(v: any) {
    this._selectMode = coerceBooleanProperty(v);

    // reset all previous profile selections
    if (!this._selectMode) {
      this.profiles.forEach((profile) => (profile.selected = false));
    }
  }
  get selectMode() {
    return this._selectMode;
  }
  private _selectMode = false;

  get selectedProfileCount() {
    return this.profiles.reduce(
      (sum, profile) => (profile.selected ? sum + 1 : sum),
      0
    );
  }

  /** Observable emitting the search term */
  private onSearch = new BehaviorSubject('');

  /** TrackBy function for the profiles. */
  trackProfile: TrackByFunction<LocalAppProfile> = trackById;

  constructor(
    private profileService: AppProfileService,
    private changeDetector: ChangeDetectorRef,
    private searchService: FuzzySearchService,
    private netquery: Netquery,
    private dialog: SfngDialogService,
    private actionIndicator: ActionIndicatorService,
    private router: Router
  ) { }

  handleProfileClick(profile: LocalAppProfile, event: MouseEvent) {
    if (event.shiftKey) {
      // stay on the same page as clicking the app actually triggers
      // a navigation before this handler is executed.
      this.router.navigate(['/app/overview']);

      this.selectMode = true;

      event.preventDefault();
      event.stopImmediatePropagation();
      event.stopPropagation();
    }

    if (this.selectMode) {
      profile.selected = !profile.selected;
    }

    if (event.shiftKey && this.selectedProfileCount === 0) {
      this.selectMode = false;
    }
  }

  importProfile() {
    const importConfig: ImportConfig = {
      type: 'profile',
      key: '',
    };

    this.dialog.create(ImportDialogComponent, {
      data: importConfig,
      autoclose: false,
      backdrop: 'light',
    });
  }

  openMergeDialog() {
    this.dialog.create(MergeProfileDialogComponent, {
      autoclose: true,
      backdrop: 'light',
      data: this.profiles.filter((p) => p.selected),
    });

    this.selectMode = false;
  }

  deleteSelectedProfiles() {
    this.dialog
      .confirm({
        header: 'Confirm Profile Deletion',
        message: `Are you sure you want to delete all ${this.selectedProfileCount} selected profiles?`,
        caption: 'Attention',
        buttons: [
          {
            id: 'no',
            text: 'Cancel',
            class: 'outline',
          },
          {
            id: 'yes',
            text: 'Delete',
            class: 'danger',
          },
        ],
      })
      .onAction('yes', () => {
        forkJoin(
          this.profiles
            .filter((profile) => profile.selected)
            .map((p) => this.profileService.deleteProfile(p))
        ).subscribe({
          next: () => {
            this.actionIndicator.success(
              'Selected Profiles Delete',
              'All selected profiles have been deleted'
            );
          },
          error: (err) => {
            this.actionIndicator.error(
              'Failed To Delete Profiles',
              `An error occured while deleting some profiles: ${this.actionIndicator.getErrorMessgae(
                err
              )}`
            );
          },
        });
      })
      .onClose.subscribe(() => (this.selectMode = false));
  }

  ngOnInit() {
    // watch all profiles and re-emit (debounced) when the user
    // enters or chanages the search-text.
    this.subscription = combineLatest([
      this.profileService.watchProfiles(),
      this.onSearch.pipe(debounceTime(100), startWith('')),
      this.netquery.getActiveProfileIDs().pipe(startWith([] as string[])),
    ]).subscribe(([profiles, searchTerm, activeProfiles]) => {
      this.loading = false;

      // find all profiles that match the search term. For searchTerm="" thsi
      // will return all profiles.
      const filtered = this.searchService.searchList(profiles, searchTerm, {
        ignoreLocation: true,
        ignoreFieldNorm: true,
        threshold: 0.1,
        minMatchCharLength: 3,
        keys: ['Name', 'PresentationPath'],
      });

      // create a lookup map of all profiles we already loaded so we don't loose
      // selection state when a profile has been updated.
      const oldProfiles = new Map<string, LocalAppProfile>(
        this.profiles.map((profile) => [
          `${profile.Source}/${profile.ID}`,
          profile,
        ])
      );

      // Prepare new, empty lists for our groups
      this.profiles = [];
      this.runningProfiles = [];
      this.recentlyEdited = [];

      // calcualte the threshold for "recently-used" (1 week).
      const recentlyUsedThreshold =
        new Date().valueOf() / 1000 - 60 * 60 * 24 * 7;

      // flatten the filtered profiles, sort them by name and group them into
      // our "app-groups" (active, recentlyUsed, others)
      this.total = filtered.length;
      filtered
        .map((item) => item.item)
        .sort((a, b) => {
          const aName = a.Name.toLocaleLowerCase();
          const bName = b.Name.toLocaleLowerCase();

          if (aName > bName) {
            return 1;
          }

          if (aName < bName) {
            return -1;
          }

          return 0;
        })
        .forEach((profile) => {
          const local: LocalAppProfile = {
            ...profile,
            hasConfigChanges:
              profile.LastEdited > 0 && Object.keys(profile.Config || {}).length > 0,
            selected:
              oldProfiles.get(`${profile.Source}/${profile.ID}`)?.selected ||
              false,
          };

          if (activeProfiles.includes(profile.Source + '/' + profile.ID)) {
            this.runningProfiles.push(local);
          } else if (profile.LastEdited >= recentlyUsedThreshold) {
            this.recentlyEdited.push(local);
          }

          // we always add the profile to "All Apps"
          this.profiles.push(local);
        });

      this.changeDetector.markForCheck();
    });
  }

  /**
   * @private
   *
   * Used as an ngModelChange callback on the search-input.
   *
   * @param term The search term entered by the user
   */
  searchApps(term: string) {
    this.searchTerm = term;
    this.onSearch.next(term);
  }

  /**
   * @private
   *
   * Opens the create profile dialog
   */
  createProfile() {
    const ref = this.dialog.create(EditProfileDialog, {
      backdrop: true,
      autoclose: false,
    });

    ref.onClose.pipe(filter((action) => action === 'saved')).subscribe(() => {
      // reset the search and reload to make sure the new
      // profile shows up
      this.searchApps('');
    });
  }

  ngOnDestroy() {
    this.subscription.unsubscribe();
  }
}
