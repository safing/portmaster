import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, EventEmitter, Input, OnChanges, OnInit, Output, SimpleChanges, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { BoolSetting, ConfigService, Setting, StringArraySetting, getActualValue } from "@safing/portmaster-api";
import { SaveSettingEvent } from "src/app/shared/config/generic-setting/generic-setting";

const configKeys = {
  splitTunUse: 'splittun/use',
  splitTunEnable: 'splittun/enable',
  splitTunUsagePolicy: 'splittun/usagePolicy',
  spnUse: 'spn/use',
  spnEnable: 'spn/enable',
  spnUsagePolicy: 'spn/usagePolicy',
} as const;

@Component({
  selector: 'app-qs-use-splittun',
  templateUrl: './qs-use-splittun.html',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class QuickSettingUseSplitTunButtonComponent implements OnInit, OnChanges {
  private destroyRef = inject(DestroyRef);

  @Input()
  settings: Setting[] = [];

  @Output()
  save = new EventEmitter<SaveSettingEvent>();

  currentValue = false;

  /** App-level settings (Exclude rules in usagePolicy) that may interfere. */
  interferingSettings: Setting[] = [];

  /** Whether the Split Tunneling module is globally enabled. null = not yet loaded. */
  splitTunModuleEnabled: boolean | null = null;

  /** Whether SPN is enabled for this app — overrides split tunnel for SPN-routed connections. */
  spnEnabled = false;

  /** Whether SPN fully overrides split tunnel: SPN in use with no Exclude rules in spn/usagePolicy. */
  spnFullOverride = false;

  /** Whether the SPN module is globally enabled. */
  private spnModuleEnabled = false;

  constructor(
    private configService: ConfigService,
    private cdr: ChangeDetectorRef
  ) { }

  ngOnChanges(changes: SimpleChanges): void {
    if ('settings' in changes) {
      this.currentValue = false;

      const useSetting = this.settings.find(s => s.Key === configKeys.splitTunUse) as BoolSetting | undefined;
      if (useSetting) {
        this.currentValue = getActualValue(useSetting);
      }

      this.updateInterfering();
    }
  }

  ngOnInit(): void {
    this.configService.watch<BoolSetting>(configKeys.splitTunEnable)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(value => {
        this.splitTunModuleEnabled = !!value;
        this.updateInterfering();
        this.cdr.markForCheck();
      });

    this.configService.watch<BoolSetting>(configKeys.spnEnable)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(value => {
        this.spnModuleEnabled = !!value;
        this.updateInterfering();
        this.cdr.markForCheck();
      });
  }

  updateUseSplitTun(enabled: boolean): void {
    this.save.next({
      isDefault: false,
      key: configKeys.splitTunUse,
      value: enabled,
    });
  }

  private updateInterfering(): void {
    this.interferingSettings = [];
    this.spnEnabled = false;
    this.spnFullOverride = false;

    if (!this.currentValue || !this.splitTunModuleEnabled) {
      return;
    }

    const spnUseSetting = this.settings.find(s => s.Key === configKeys.spnUse) as BoolSetting | undefined;
    this.spnEnabled = this.spnModuleEnabled && !!spnUseSetting && !!getActualValue(spnUseSetting);

    // If SPN is enabled, check if it fully overrides Split Tunnel (no Exclude rules in SPN policy)
    if (this.spnEnabled) {
      const spnPolicy = this.settings.find(s => s.Key === configKeys.spnUsagePolicy) as StringArraySetting | undefined;
      const spnPolicyValue = spnPolicy ? getActualValue(spnPolicy) : [];
      const hasSpnExcludeRules = Array.isArray(spnPolicyValue) && spnPolicyValue.some(rule => rule.startsWith('- ') || rule === '-');
      this.spnFullOverride = !hasSpnExcludeRules;
    }

    // Exclude rules in usagePolicy may prevent some connections from being tunneled
    const usagePolicy = this.settings.find(s => s.Key === configKeys.splitTunUsagePolicy) as StringArraySetting | undefined;
    if (usagePolicy) {
      const value = getActualValue(usagePolicy);
      if (Array.isArray(value) && value.some(rule => rule.startsWith('- ') || rule === '-')) {
        this.interferingSettings.push(usagePolicy);
      }
    }
  }
}
