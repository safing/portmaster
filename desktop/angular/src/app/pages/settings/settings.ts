import { Component, OnDestroy, OnInit } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { ConfigService, Setting } from '@safing/portmaster-api';
import { Subscription } from 'rxjs';
import { StatusService, VersionStatus } from 'src/app/services';
import { ActionIndicatorService } from 'src/app/shared/action-indicator';
import { fadeInAnimation } from 'src/app/shared/animations';
import { SaveSettingEvent } from 'src/app/shared/config/generic-setting/generic-setting';

@Component({
  templateUrl: './settings.html',
  styleUrls: [
    '../page.scss',
    './settings.scss'
  ],
  animations: [fadeInAnimation]
})
export class SettingsComponent implements OnInit, OnDestroy {
  /** @private The current search term for the settings. */
  searchTerm: string = '';

  /** @private All settings currently displayed. */
  settings: Setting[] = [];

  /** @private The available and selected resource versions. */
  versions: VersionStatus | null = null;

  /**
   * @private
   * The key of the setting to highligh, if any ...
   */
  highlightSettingKey: string | null = null;

  /** Subscription to watch all available settings. */
  private subscription = Subscription.EMPTY;

  constructor(
    public configService: ConfigService,
    public statusService: StatusService,
    private actionIndicator: ActionIndicatorService,
    private route: ActivatedRoute,
  ) { }

  ngOnInit(): void {
    this.subscription = new Subscription();

    this.loadSettings();

    // Request the current resource versions once. We add
    // it to the subscription to prevent a memory leak in
    // case the user leaves the page before the versions
    // have been loaded.
    const versionSub = this.statusService.getVersions()
      .subscribe(version => this.versions = version);

    this.subscription.add(versionSub);

    const querySub = this.route.queryParamMap
      .subscribe(
        params => {
          this.highlightSettingKey = params.get('setting');
        }
      )
    this.subscription.add(querySub);
  }

  ngOnDestroy() {
    this.subscription.unsubscribe();
  }

  /**
   * Loads all settings from the portmaster.
   */
  private loadSettings() {
    const configSub = this.configService.query('')
      .subscribe(settings => this.settings = settings);
    this.subscription.add(configSub);
  }

  /**
   * @private
   * SaveSettingEvent is emitted by the settings-view
   * component when a value has been changed and should be saved.
   *
   * @param event The save-settings event
   */
  saveSetting(event: SaveSettingEvent) {
    let idx = this.settings.findIndex(setting => setting.Key === event.key);
    if (idx < 0) {
      return;
    }

    const setting = {
      ...this.settings[idx],
    }

    if (event.isDefault) {
      delete (setting['Value']);
    } else {
      setting.Value = event.value;
    }

    this.configService.save(setting)
      .subscribe({
        next: () => {
          if (!!event.accepted) {
            event.accepted();
          }

          this.settings[idx] = setting;

          // copy the settings into a new array so we trigger
          // an input update due to changed array identity.
          this.settings = [...this.settings];

          // for the release level setting we need to
          // to a page-reload since portmaster will now
          // return more settings.
          if (setting.Key === 'core/releaseLevel') {
            this.loadSettings();
          }
        },
        error: err => {
          if (!!event.rejected) {
            event.rejected(err);
          }

          this.actionIndicator.error('Failed to save setting', err);
          console.error(err);
        }
      })
  }
}
