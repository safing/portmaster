import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ScrollDispatcher } from '@angular/cdk/overlay';
import {
  AfterViewInit,
  ChangeDetectorRef,
  Component,
  ElementRef,
  EventEmitter,
  Input,
  OnDestroy,
  OnInit,
  Output,
  QueryList,
  TrackByFunction,
  ViewChildren,
} from '@angular/core';
import {
  ConfigService,
  ExpertiseLevelNumber,
  PortapiService,
  Setting,
  StringSetting,
  releaseLevelFromName,
} from '@safing/portmaster-api';
import { BehaviorSubject, Subscription, combineLatest } from 'rxjs';
import { debounceTime } from 'rxjs/operators';
import { StatusService } from 'src/app/services';
import {
  fadeInAnimation,
  fadeInListAnimation,
  fadeOutAnimation,
} from 'src/app/shared/animations';
import { FuzzySearchService } from 'src/app/shared/fuzzySearch';
import { ExpertiseLevelOverwrite } from '../expertise/expertise-directive';
import { SaveSettingEvent } from './generic-setting/generic-setting';
import { ActionIndicatorService } from '../action-indicator';
import { SfngDialogService } from '@safing/ui';
import {
  ExportConfig,
  ExportDialogComponent,
} from './export-dialog/export-dialog.component';
import {
  ImportConfig,
  ImportDialogComponent,
} from './import-dialog/import-dialog.component';

import { subsystems, SubsystemWithExpertise } from './subsystems'

interface Category {
  name: string;
  settings: Setting[];
  minimumExpertise: ExpertiseLevelNumber;
  collapsed: boolean;
  hasUserDefinedValues: boolean;
}

@Component({
  selector: 'app-settings-view',
  templateUrl: './config-settings.html',
  styleUrls: ['./config-settings.scss'],
  animations: [fadeInAnimation, fadeOutAnimation, fadeInListAnimation],
})
export class ConfigSettingsViewComponent
  implements OnInit, OnDestroy, AfterViewInit {
  subsystems: SubsystemWithExpertise[] = subsystems;
  others: Setting[] | null = null;
  settings: Map<string, Category[]> = new Map();

  /** A list of all selected settings for export */
  selectedSettings: { [key: string]: boolean } = {};

  /** Whether or not we are currently in "export" mode */
  exportMode = false;

  activeSection = '';
  activeCategory = '';
  loading = true;

  @Input()
  resetLabelText = 'Reset to system default';

  @Input()
  set compactView(v: any) {
    this._compactView = coerceBooleanProperty(v);
  }
  get compactView() {
    return this._compactView;
  }
  private _compactView = false;

  @Input()
  set lockDefaults(v: any) {
    this._lockDefaults = coerceBooleanProperty(v);
  }
  get lockDefaults() {
    return this._lockDefaults;
  }
  private _lockDefaults = false;

  @Input()
  set userSettingsMarker(v: any) {
    this._userSettingsMarker = coerceBooleanProperty(v);
  }
  get userSettingsMarker() {
    return this._userSettingsMarker;
  }
  private _userSettingsMarker = true;

  @Input()
  set searchTerm(v: string) {
    this.onSearch.next(v);
  }

  @Input()
  set availableSettings(v: Setting[]) {
    this.onSettingsChange.next(v);
  }

  @Input()
  set scope(scope: 'global' | string) {
    this._scope = scope;
  }
  get scope() {
    return this._scope;
  }
  private _scope: 'global' | string = 'global';

  @Input()
  displayStackable: string | boolean = false;

  @Input()
  set highlightKey(key: string | null) {
    this._highlightKey = key || null;
    this._scrolledToHighlighted = false;
    // If we already loaded the settings then instruct the window
    // to scroll the setting into the view.
    if (!!key && !!this.settings && this.settings.size > 0) {
      this.scrollTo(key);
      this._scrolledToHighlighted = true;
    }
  }
  get highlightKey() {
    return this._highlightKey;
  }
  private _highlightKey: string | null = null;
  private _scrolledToHighlighted = false;

  mustShowSetting: ExpertiseLevelOverwrite<Setting> = (
    lvl: ExpertiseLevelNumber,
    s: Setting
  ) => {
    if (lvl >= s.ExpertiseLevel) {
      // this setting is shown anyway.
      return false;
    }
    if (s.Key === this.highlightKey) {
      return true;
    }
    // the user is searching for settings so make sure we even show advanced or developer settings
    if (this.onSearch.getValue() !== '') {
      return true;
    }
    if (s.Value === undefined) {
      // no value set
      return false;
    }
    return true;
  };

  mustShowCategory: ExpertiseLevelOverwrite<Category> = (
    lvl: ExpertiseLevelNumber,
    cat: Category
  ) => {
    return cat.settings.some((setting) => this.mustShowSetting(lvl, setting));
  };

  mustShowSubsystem: ExpertiseLevelOverwrite<SubsystemWithExpertise> = (
    lvl: ExpertiseLevelNumber,
    subsys: SubsystemWithExpertise
  ) => {
    return !!this.settings
      .get(subsys.ConfigKeySpace)
      ?.some((cat) => this.mustShowCategory(lvl, cat));
  };

  @Output()
  save = new EventEmitter<SaveSettingEvent>();

  private onSearch = new BehaviorSubject<string>('');
  private onSettingsChange = new BehaviorSubject<Setting[]>([]);

  @ViewChildren('navLink', { read: ElementRef })
  navLinks: QueryList<ElementRef> | null = null;

  private subscription = Subscription.EMPTY;

  constructor(
    public statusService: StatusService,
    public configService: ConfigService,
    private elementRef: ElementRef,
    private changeDetectorRef: ChangeDetectorRef,
    private scrollDispatcher: ScrollDispatcher,
    private searchService: FuzzySearchService,
    private actionIndicator: ActionIndicatorService,
    private portapi: PortapiService,
    private dialog: SfngDialogService,
  ) { }

  openImportDialog() {
    const importConfig: ImportConfig = {
      type: 'setting',
      key: this.scope,
    };
    this.dialog.create(ImportDialogComponent, {
      data: importConfig,
      autoclose: false,
      backdrop: 'light',
    });
  }

  toggleExportMode() {
    this.exportMode = !this.exportMode;

    if (this.exportMode) {
      this.actionIndicator.info(
        'Settings Export',
        'Please select all settings you want to export and press "Save" to generate the export. Note that settings with system defaults cannot be exported and are hidden.'
      );
    }
  }

  generateExport() {
    let selectedKeys = Object.keys(this.selectedSettings).reduce((sum, key) => {
      if (this.selectedSettings[key]) {
        sum.push(key);
      }

      return sum;
    }, [] as string[]);

    if (selectedKeys.length === 0) {
      selectedKeys = Array.from(this.settings.values()).reduce(
        (sum, current) => {
          current.forEach((cat) => {
            cat.settings.forEach((s) => {
              if (s.Value !== undefined) {
                sum.push(s.Key);
              }
            });
          });

          return sum;
        },
        [] as string[]
      );
    }

    this.portapi.exportSettings(selectedKeys, this.scope).subscribe({
      next: (exportBlob) => {
        const exportConfig: ExportConfig = {
          type: 'setting',
          content: exportBlob,
        };

        this.dialog.create(ExportDialogComponent, {
          data: exportConfig,
          backdrop: 'light',
          autoclose: true,
        });

        this.exportMode = false;
      },
      error: (err) => {
        const msg = this.actionIndicator.getErrorMessgae(err);
        this.actionIndicator.error('Failed To Generate Export', msg);
      },
    });
  }

  saveSetting(event: SaveSettingEvent, s: Setting) {
    this.save.next(event);
    const subsys = this.subsystems.find(
      (subsys) => s.Key === subsys.ToggleOptionKey
    );
    if (!!subsys) {
      // trigger a reload of the page as we now might need to show more
      // settings.
      this.onSettingsChange.next(this.onSettingsChange.getValue());
    }
  }

  trackSubsystem: TrackByFunction<SubsystemWithExpertise> =
    this.statusService.trackSubsystem;

  trackCategory(_: number, cat: Category) {
    return cat.name;
  }

  ngOnInit(): void {
    this.subscription = combineLatest([
      this.onSettingsChange,
      this.onSearch.pipe(debounceTime(250)),
      this.configService.watch<StringSetting>('core/releaseLevel'),
    ])
      .pipe(debounceTime(10))
      .subscribe(
        ([settings, searchTerm, currentReleaseLevelSetting]) => {
          this.others = [];
          this.settings = new Map();

          // Get the current release level as a number (fallback to 'stable' is something goes wrong)
          const currentReleaseLevel = releaseLevelFromName(
            currentReleaseLevelSetting || ('stable' as any)
          );

          // Make sure we only display settings that are allowed by the releaselevel setting.
          settings = settings.filter(
            (setting) => setting.ReleaseLevel <= currentReleaseLevel
          );

          // Use fuzzy-search to limit the number of settings shown.
          const filtered = this.searchService.searchList(settings, searchTerm, {
            ignoreLocation: true,
            ignoreFieldNorm: true,
            threshold: 0.1,
            minMatchCharLength: 3,
            keys: [
              { name: 'Name', weight: 3 },
              { name: 'Description', weight: 2 },
            ],
          });

          // The search service wraps the items in a search-result object.
          // Unwrap them now.
          settings = filtered.map((res) => res.item);

          // use order-annotations to sort the settings. This affects the order of
          // the categories as well as the settings inside the categories.
          settings.sort((a, b) => {
            const orderA = a.Annotations?.['safing/portbase:ui:order'] || 0;
            const orderB = b.Annotations?.['safing/portbase:ui:order'] || 0;
            return orderA - orderB;
          });

          settings.forEach((setting) => {
            let pushed = false;
            this.subsystems.forEach((subsys) => {
              if (
                setting.Key.startsWith(
                  subsys.ConfigKeySpace.slice('config:'.length)
                )
              ) {
                // get the category name annotation and fallback to 'others'
                let catName = 'other';
                if (
                  !!setting.Annotations &&
                  !!setting.Annotations['safing/portbase:ui:category']
                ) {
                  catName = setting.Annotations['safing/portbase:ui:category'];
                }

                // ensure we have a category array for the subsystem.
                let categories = this.settings.get(subsys.ConfigKeySpace);
                if (!categories) {
                  categories = [];
                  this.settings.set(subsys.ConfigKeySpace, categories);
                }

                // find or create the appropriate category object.
                let cat = categories.find((c) => c.name === catName);
                if (!cat) {
                  cat = {
                    name: catName,
                    minimumExpertise: ExpertiseLevelNumber.developer,
                    settings: [],
                    collapsed: false,
                    hasUserDefinedValues: false,
                  };
                  categories.push(cat);
                }

                // add the setting to the category object and update
                // the minimum expertise required for the category.
                cat.settings.push(setting);
                if (setting.ExpertiseLevel < cat.minimumExpertise) {
                  cat.minimumExpertise = setting.ExpertiseLevel;
                }

                pushed = true;
              }
            });

            // if we did not push the setting to some subsystem
            // we need to push it to "others"
            if (!pushed) {
              this.others!.push(setting);
            }
          });

          if (this.others.length === 0) {
            this.others = null;
          }

          // Reduce the subsystem array to only contain subsystems that
          // actually have settings to show.
          // Also update the minimumExpertiseLevel for those subsystems
          this.subsystems = this.subsystems
            .filter((subsys) => {
              return !!this.settings.get(subsys.ConfigKeySpace);
            })
            .map((subsys) => {
              let categories = this.settings.get(subsys.ConfigKeySpace)!;
              let hasUserDefinedValues = false;
              categories.forEach((c) => {
                c.hasUserDefinedValues = c.settings.some(
                  (s) => s.Value !== undefined
                );
                hasUserDefinedValues =
                  c.hasUserDefinedValues || hasUserDefinedValues;
              });

              subsys.hasUserDefinedValues = hasUserDefinedValues;

              let toggleOption: Setting | undefined = undefined;
              for (let c of categories) {
                toggleOption = c.settings.find(
                  (s) => s.Key === subsys.ToggleOptionKey
                );
                if (!!toggleOption) {
                  if (
                    (toggleOption.Value !== undefined && !toggleOption.Value) ||
                    (toggleOption.Value === undefined &&
                      !toggleOption.DefaultValue)
                  ) {
                    subsys.isDisabled = true;

                    // remove all settings for all subsystem categories
                    // except for the ToggleOption.
                    categories = categories
                      .map((c) => ({
                        ...c,
                        settings: c.settings.filter(
                          (s) => s.Key === toggleOption!.Key
                        ),
                      }))
                      .filter((cat) => cat.settings.length > 0);
                    this.settings.set(subsys.ConfigKeySpace, categories);
                  }
                  break;
                }
              }

              // reduce the categories to find the smallest expertise level requirement.
              subsys.minimumExpertise = categories.reduce((min, current) => {
                if (current.minimumExpertise < min) {
                  return current.minimumExpertise;
                }
                return min;
              }, ExpertiseLevelNumber.developer as ExpertiseLevelNumber);

              return subsys;
            });

          // Force the core subsystem to the end.
          if (this.subsystems.length >= 2 && this.subsystems[0].ID === 'core') {
            this.subsystems.push(
              this.subsystems.shift() as SubsystemWithExpertise
            );
          }

          // Notify the user interface that we're done loading
          // the settings.
          this.loading = false;

          // If there's a highlightKey set and we have not yet scrolled
          // to it (because it was set during component bootstrap) we
          // need to scroll there now.
          if (this._highlightKey !== null && !this._scrolledToHighlighted) {
            this._scrolledToHighlighted = true;

            // Use the next animation frame for scrolling
            window.requestAnimationFrame(() => {
              this.scrollTo(this._highlightKey || '');
            });
          }
        }
      );
  }

  ngAfterViewInit() {
    this.subscription = new Subscription();

    // Whenever our scroll-container is scrolled we might
    // need to update which setting is currently highlighted
    // in the settings-navigation.
    this.subscription.add(
      this.scrollDispatcher
        .scrolled(10)
        .subscribe(() => this.intersectionCallback())
    );

    // Also, entries in the settings-navigation might become
    // visible with expertise/release level changes so make
    // sure to recalculate the current one whenever a change
    // happens.
    this.subscription.add(
      this.navLinks?.changes.subscribe(() => {
        this.intersectionCallback();
        this.changeDetectorRef.detectChanges();
      })
    );
  }

  ngOnDestroy() {
    this.subscription.unsubscribe();
    this.onSearch.complete();
  }

  /**
   * Calculates which navigation entry should be highlighted
   * depending on the scroll position.
   */
  private intersectionCallback() {
    // search our parents for the element that's scrollable
    let elem: HTMLElement = this.elementRef.nativeElement;
    while (!!elem) {
      if (elem.scrollTop > 0) {
        break;
      }
      elem = elem.parentElement!;
    }

    // if there's no scrolled/scrollable parent element
    // our content itself is scrollable so use our own
    // host element as the anchor for the calculation.
    if (!elem) {
      elem = this.elementRef.nativeElement;
    }

    // get the elements offset to page-top
    var offsetTop = 0;
    if (!!elem) {
      const viewRect = elem.getBoundingClientRect();
      offsetTop = viewRect.top;
    }

    this.navLinks?.some((link) => {
      const subsystem = link.nativeElement.getAttribute('subsystem');
      const category = link.nativeElement.getAttribute('category');

      const lastChild = (link.nativeElement as HTMLElement)
        .lastElementChild as HTMLElement;
      if (!lastChild) {
        return false;
      }

      const rect = lastChild.getBoundingClientRect();
      const styleBox = getComputedStyle(lastChild);

      const offset =
        rect.top +
        rect.height -
        parseInt(styleBox.marginBottom) -
        parseInt(styleBox.paddingBottom);

      if (offset >= offsetTop) {
        this.activeSection = subsystem;
        this.activeCategory = category;
        return true;
      }

      return false;
    });
    this.changeDetectorRef.detectChanges();
  }

  /**
   * @private
   * Performs a smooth-scroll to the given anchor element ID.
   *
   * @param id The ID of the anchor element to scroll to.
   */
  scrollTo(id: string, cat?: Category) {
    if (!!cat) {
      cat.collapsed = false;
    }
    document.getElementById(id)?.scrollIntoView({
      behavior: 'smooth',
      block: 'start',
      inline: 'nearest',
    });
  }
}
