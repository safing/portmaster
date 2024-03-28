import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { TemplatePortal } from '@angular/cdk/portal';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, EventEmitter, HostBinding, Input, OnInit, Output, TemplateRef, ViewChild, ViewContainerRef } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { NgModel } from '@angular/forms';
import { BaseSetting, ConfigService, ExpertiseLevel, ExpertiseLevelNumber, ExternalOptionHint, OptionType, PortapiService, QuickSetting, ReleaseLevel, SPNService, SettingValueType, UserProfile, WellKnown, applyQuickSetting } from '@safing/portmaster-api';
import { SfngDialogRef, SfngDialogService } from '@safing/ui';
import { Button } from 'js-yaml-loader!../../../i18n/helptexts.yaml';
import { Subject } from 'rxjs';
import { debounceTime, tap } from 'rxjs/operators';
import { ActionIndicatorService } from '../../action-indicator';
import { fadeInAnimation, fadeOutAnimation } from '../../animations';
import { ExpertiseService } from '../../expertise/expertise.service';
import { SPNAccountDetailsComponent } from '../../spn-account-details';

export interface SaveSettingEvent<S extends BaseSetting<any, any> = any> {
  key: string;
  value: SettingValueType<S>;
  isDefault: boolean;
  rejected?: (err: any) => void
  accepted?: () => void
}

@Component({
  selector: 'app-generic-setting',
  templateUrl: './generic-setting.html',
  exportAs: 'appGenericSetting',
  styleUrls: ['./generic-setting.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
  animations: [
    fadeInAnimation,
    fadeOutAnimation
  ]
})
export class GenericSettingComponent<S extends BaseSetting<any, any>> implements OnInit {
  //
  // Constants used in the template.
  //

  readonly optionHint = ExternalOptionHint;
  readonly expertiseNames = ExpertiseLevel
  readonly expertise = ExpertiseLevelNumber;
  readonly optionType = OptionType;
  readonly releaseLevel = ReleaseLevel;
  readonly wellKnown = WellKnown;

  @ViewChild('helpTemplate', { read: TemplateRef, static: true })
  helpTemplate: TemplateRef<any> | null = null;
  private helpDialogRef: SfngDialogRef<any> | null = null;

  // Whether or not the user needs to upgrade his/her account before
  // this setting is valid.
  _upgradeRequired = false;

  /**
   * Whether or not the component/setting is disabled and should
   * be read-only.
   */
  @Input()
  @HostBinding('class.disabled')
  set disabled(v: any) {
    this._disabled = coerceBooleanProperty(v);
  }
  get disabled() {
    return this._disabled || this._upgradeRequired;
  }
  private _disabled: boolean = false;

  /** Returns the symbolMap annoation for endpoint-lists */
  get symbolMap() {
    return this.setting?.Annotations[WellKnown.EndpointListVerdictNames] || {
      '+': 'Allow',
      '-': 'Block'
    };
  }

  /** Whether or not the setting should be in select mode */
  @Input()
  set selectMode(v: any) {
    this._selectMode = coerceBooleanProperty(v)

    if (!this.selectMode) {
      this.selected = false;
      this.selectedChange.next(false);
    }
  }
  get selectMode() { return this._selectMode }
  private _selectMode = false;

  /** Whether or not the setting has been selected */
  @Input()
  set selected(v: any) {
    this._selected = coerceBooleanProperty(v)
  }
  get selected() { return this._selected }
  private _selected = false;

  /** Emits when the user (de-) selectes the setting. Can be used for two-way binding */
  @Output()
  selectedChange = new EventEmitter<boolean>();

  /** Controls whether or not header with the setting name and success/failure markers is shown */
  @Input()
  set showHeader(v: any) {
    this._showHeader = coerceBooleanProperty(v);
  }
  get showHeader() { return this._showHeader }
  private _showHeader = true;

  /** Controls whether or not the blue or red status borders are shown */
  @Input()
  set enableActiveBorder(v: any) {
    this._enableActiveBorder = coerceBooleanProperty(v);
  }
  get enableActiveBorder() { return this._enableActiveBorder }
  private _enableActiveBorder = true;

  /**
   * Whether or not the component should be displayed as "locked"
   * when the default value is used (that is, no 'Value' property
   * in the setting)
   */
  @Input()
  set lockDefaults(v: any) {
    this._lockDefaults = coerceBooleanProperty(v);
  }
  get lockDefaults() {
    return this._lockDefaults;
  }
  private _lockDefaults: boolean = false;

  /** The label to display in the reset-value button */
  @Input()
  resetLabelText = 'Reset';

  /** Emits an event whenever the setting should be saved. */
  @Output()
  save = new EventEmitter<SaveSettingEvent<S>>();

  /** Wether or not stackable values should be displayed. */
  @Input()
  set displayStackable(v: any) {
    this._displayStackable = coerceBooleanProperty(v);
  }
  get displayStackable() {
    return this._displayStackable;
  }
  private _displayStackable = false;

  /**
   * Whether or not the help text is currently shown
   */
  @Input()
  set showHelp(v: any) {
    this._showHelp = coerceBooleanProperty(v);
  }
  get showHelp() {
    return this._showHelp;
  }
  private _showHelp = false;

  /** Used internally to publish save events. */
  private triggerSave = new Subject<void>();

  /** Whether or not the value was reset. */
  wasReset = false;

  /** Whether or not a save request was rejected */
  @HostBinding('class.rejected')
  get rejected() {
    return this._rejected;
  }
  private _rejected = null;

  @HostBinding('class.saved')
  get changeAccepted() {
    return this._changeAccepted;
  }
  private _changeAccepted = false;

  /**
   * @private
   * Returns the external option type hint from a setting.
   *
   * @param opt The setting for with to return the external option hint
   */
  externalOptType(opt: S | null): ExternalOptionHint | null {
    return opt?.Annotations?.[WellKnown.DisplayHint] || null;
  }

  /**
   * @private
   * Returns whether or not a restart is pending for this setting
   * to apply.
   */
  get restartPending(): boolean {
    return !!this._setting?.Annotations?.[WellKnown.RestartPending];
  }

  /**
   * @private
   * Returns whether or not a UI reload is required for this setting
   * to apply
   */
  get uiReloadRequired(): boolean {
    return this._setting?.Annotations?.[WellKnown.RequiresUIReload] !== undefined;
  }

  /**
   * Returns true if the setting has been touched (modified) by the user
   * since the component has been rendered.
   */
  @HostBinding('class.touched')
  get touched() {
    return this._touched;
  }
  private _touched = false;

  /**
   * Returns true if the settings is currently locked.
   */
  @HostBinding('class.locked')
  get isLocked() {
    return (this.wasReset || !this.userConfigured) && this.lockDefaults;
  }

  /**
   * Returns true if the user has configured the setting on their
   * own or if the default value is being used.
   */
  @HostBinding('class.changed')
  get userConfigured() {
    return this.setting?.Value !== undefined;
  }

  /**
   * Returns true if the setting is dirty. That is, the user
   * has changed the setting in the view but it has not yet
   * been saved.
   */
  @HostBinding('class.dirty')
  get dirty() {
    if (typeof this._currentValue !== 'object') {
      return this._currentValue !== this._savedValue;
    }
    // JSON object (OptionType.StringArray) require will
    // not be the same reference so we need to compare their
    // string representations. That's a bit more costly but should
    // still be fast enough.
    // TODO(ppacher): calculate this only when required.
    return JSON.stringify(this._currentValue) !== JSON.stringify(this._savedValue)
  }

  /**
   * Returns true if the setting is pristine. That is, the
   * settings default value is used and the user has not yet
   * changed the value inside the view.
   */
  @HostBinding('class.pristine')
  get pristine() {
    return !this.dirty && !this.userConfigured
  }

  /** A list of buttons for the tip-up */
  sfngTipUpButtons: Button[] = [];

  /**
   * Unlock the setting if it is locked. Unlocking will
   * emit the default value to be safed for the setting.
   */
  unlock() {
    if (!this.isLocked || !this.setting) {
      return;
    }

    this._touched = true;
    this.wasReset = false;
    let value = this.defaultValue;

    if (this.stackable) {
      // TODO(ppacher): fix this one once string[] options can be
      // stackable
      value = [] as SettingValueType<S>;
    }

    this.updateValue(value, true);
    // update the settings value now so the UI
    // responds immediately.
    this.setting!.Value = value;
  }

  /** True if the current setting is stackable */
  get stackable() {
    return !!this.setting?.Annotations[WellKnown.Stackable];
  }

  /** Wether or not stackable values should be shown right now */
  get showStackable() {
    return this.stackable && this.displayStackable;
  }

  /**
   * @private
   * Toggle Whether or not the help text is displayed
   */
  toggleHelp() {
    this.showHelp = !this.showHelp;
  }

  /**
   * @private
   * Toggle Whether or not the setting is currently locked.
   */
  toggleLock() {
    if (this.isLocked) {
      this.unlock();
      return;
    }

    this.resetValue();
  }

  /**
   * @private
   * Closes the help dialog.
   */
  closeHelpDialog() {
    this.helpDialogRef?.close();
  }

  @ViewChild(NgModel, { static: false })
  model: NgModel | null = null;

  /**
   * The actual setting that should be managed.
   * The setter also updates the "currently" used
   * value (which is either user configured or
   * the default). See {@property userConfigured}.
   */
  @Input()
  set setting(s: S | null) {
    this.sfngTipUpButtons = [];

    this._setting = s;
    if (!s) {
      this._currentValue = null;
      return;
    }

    if (this._setting?.Help) {
      this.sfngTipUpButtons = [
        {
          name: 'Show More',
          action: {
            ID: '',
            Text: '',
            Type: 'ui',
            Run: async () => {
              if (!this.helpTemplate) {
                return;
              }

              // close any existing help dialog for THIS setting.
              if (!!this.helpDialogRef) {
                this.helpDialogRef.close();
              }

              // Create a new dialog form the helpTemplate
              const portal = new TemplatePortal(this.helpTemplate, this.viewRef);
              const ref = this.dialog.create(portal, {
                // we don't use a backdrop and make the dialog dragable so the user can
                // move it somewhere else and keep it open while configuring the setting.
                backdrop: false,
                dragable: true,
              });

              // make sure we reset the helpDialogRef to null once it get's clsoed.
              this.helpDialogRef = ref;
              this.helpDialogRef.onClose.subscribe(() => {
                // but only if helpDialogRef still points to the same
                // dialog reference. Otherwise we got closed because the user
                // opened a new one and helpDialogRef already points to the new
                // dialog.
                if (this.helpDialogRef === ref) {
                  this.helpDialogRef = null;
                }
              });
            },
            Payload: undefined,
          },
        },
      ]
    }
    this.updateActualValue();
  }
  get setting(): S | null {
    return this._setting;
  }

  /**
   * The defaultValue input allows to overwrite the default
   * value of the setting.
   */
  @Input()
  set defaultValue(val: SettingValueType<S>) {
    this._defaultValue = val;
    this.updateActualValue();
  }

  get defaultValue() {
    // Return cached value.
    if (this._defaultValue !== null) {
      return this._defaultValue;
    }

    // Stackable options are displayed differently.
    if (this.stackable) {
      if (this.setting?.GlobalDefault === undefined && this.setting?.DefaultValue !== null) {
        return this.setting?.DefaultValue;
      }
      return [] as SettingValueType<S>;
    }

    // Return global, then default value.
    if (this.setting?.GlobalDefault !== undefined) {
      return this.setting.GlobalDefault
    }
    return this.setting?.DefaultValue
  }

  /* An optional default value overwrite */
  _defaultValue: SettingValueType<S> | null = null;

  /* Whether or not the setting has been saved */
  saved = true;

  /* The settings value, updated by the setting() setter */
  _setting: S | null = null;

  /* The currently configured value. Updated by the setting() setter */
  _currentValue: SettingValueType<S> | null = null;

  /* The currently saved value. Updated by the setting() setter */
  _savedValue: SettingValueType<S> | null = null;

  /* Used to cache the value of a basic-setting because we only want to save that on blur */
  _basicSettingsValueCache: SettingValueType<S> | null = null

  /** Whether or not the network rating system is enabled. */
  networkRatingEnabled$ = this.configService.networkRatingEnabled$;

  get expertiseLevel() {
    return this.expertiseService.change;
  }

  constructor(
    private expertiseService: ExpertiseService,
    private configService: ConfigService,
    private portapi: PortapiService,
    private dialog: SfngDialogService,
    private changeDetectorRef: ChangeDetectorRef,
    private actionIndicator: ActionIndicatorService,
    private spn: SPNService,
    private viewRef: ViewContainerRef,
    private destryoRef: DestroyRef,
  ) { }

  ngOnInit() {
    this.triggerSave
      .pipe(
        debounceTime(500),
        takeUntilDestroyed(this.destryoRef),
      )
      .subscribe(() => this.emitSaveRequest())

    // watch the SPN user profile so we know which feature_ids
    // are available for the user.
    this.spn.profile$
      .pipe(takeUntilDestroyed(this.destryoRef))
      .subscribe((profile: UserProfile | null) => {
        let value = this.setting?.Annotations[WellKnown.RequiresFeatureID]
        if (value === undefined) {
          this._upgradeRequired = false;
        } else {
          if (!Array.isArray(value)) {
            value = [value];
          }

          this._upgradeRequired = value.some(val => !(profile?.current_plan?.feature_ids || []).includes(val))
        }

        this.changeDetectorRef.markForCheck();
      })
  }

  /**
   * @private
   * Resets the value of setting by discarding any user
   * configured values and reverting back to the default
   * value.
   */
  resetValue() {
    if (!this._setting) {
      return;
    }
    this._touched = true;

    this._currentValue = this.defaultValue;
    this.wasReset = true;

    this.triggerSave.next();
  }

  /**
   * @private
   * Aborts/reverts the current change to the value that's
   * already saved.
   */
  abortChange() {
    this._currentValue = this._savedValue;
    this._touched = true;
    this._rejected = null;
  }

  /**
   * @private
   * Update the current value by applying a quick-setting.
   *
   * @param qs The quick-settting to apply
   */
  applyQuickSetting(qs: QuickSetting<SettingValueType<S>>) {
    if (this.disabled) {
      return;
    }

    const value = applyQuickSetting(this._currentValue, qs);
    if (value === null) {
      return;
    }

    this.updateValue(value, true);
  }

  openAccountDetails() {
    this.dialog.create(SPNAccountDetailsComponent, {
      autoclose: true,
      backdrop: 'light'
    })
  }

  restartNow() {
    if (this._setting?.RequiresRestart) {
      this.dialog.confirm({
        header: 'Restart Portmaster',
        message: 'Do you want to restart the Portmaster now?',
        buttons: [
          {
            id: 'no',
            text: 'Maybe Later',
            class: 'outline',
          },
          {
            id: 'restart',
            text: 'Restart',
            class: 'danger'
          }
        ]
      })
        .onAction('restart', () =>
          this.portapi.restartPortmaster()
            .subscribe(this.actionIndicator.httpObserver(
              'Restarting ...',
              'Failed to Restart',
            ))
        )
        .onAction('no', () => {
          this._changeAccepted = false;
          this.changeDetectorRef.markForCheck();
        });

      return;
    }

    if (this.uiReloadRequired) {
      this.portapi.reloadUI()
        .pipe(
          tap(() => {
            setTimeout(() => window.location.reload(), 1000)
          })
        )
        .subscribe(this.actionIndicator.httpObserver(
          'Reloading UI ...',
          'Failed to Reload UI',
        ))
    }
  }

  /**
   * Emits a save request to the parent component.
   */
  private _saveInterval: any;
  private emitSaveRequest() {
    const isDefault = this.wasReset;
    let value = this._setting!['Value'];

    if (isDefault) {
      delete (this._setting!['Value']);
    } else {
      this._setting!.Value = this._currentValue;
    }


    let wasReset = this.wasReset;
    this.wasReset = false;
    this._rejected = null;
    this._changeAccepted = false;
    if (!!this._saveInterval) {
      clearTimeout(this._saveInterval);
    }

    this.save.next({
      key: this.setting!.Key,
      isDefault: isDefault,
      value: this._setting!.Value,
      rejected: (err: any) => {
        this._setting!['Value'] = value;
        this._rejected = err;
        this.changeDetectorRef.markForCheck();
      },
      accepted: () => {
        if (!wasReset) {
          this._changeAccepted = true;
          // if no restart is required fade the "✔️ Saved" out after
          // a few seconds.
          if (!this._setting?.RequiresRestart) {
            this._saveInterval = setTimeout(() => {
              this._changeAccepted = false;
              this._saveInterval = null;
              this.changeDetectorRef.markForCheck();
            }, 4000);
          }
        }

        this.changeDetectorRef.markForCheck();

      }
    })
  }

  /**
   * @private
   * Used in our view as a ngModelChange callback to
   * update the value.
   *
   * @param value The new value as emitted by the view
   */
  updateValue(value: SettingValueType<S>, save = false) {
    this._touched = true;

    this._changeAccepted = false;
    this._rejected = null;
    if (!!this._saveInterval) {
      clearTimeout(this._saveInterval);
    }

    if (save) {

      this._currentValue = value;
      this.triggerSave.next();
    } else {
      this._basicSettingsValueCache = value;
    }
  }

  /**
   * @private
   * A list of quick-settings available for the setting.
   * The getter makes sure to always return an array.
   */
  get quickSettings(): QuickSetting<SettingValueType<S>>[] {
    if (!this.setting || !this.setting.Annotations[WellKnown.QuickSetting]) {
      return [];
    }

    const quickSettings = this.setting.Annotations[WellKnown.QuickSetting]!;

    return Array.isArray(quickSettings)
      ? quickSettings
      : [quickSettings];
  }

  /**
   * Determine the current, actual value of the setting
   * by taking the settings Value, default Value or global
   * default into account.
   */
  private updateActualValue() {
    if (!this.setting) {
      return
    }

    this.wasReset = false;

    const s = this.setting;

    const value = s.Value === undefined
      ? this.defaultValue
      : s.Value;


    this._currentValue = value;
    this._savedValue = value;
    this._basicSettingsValueCache = value;
  }
}
