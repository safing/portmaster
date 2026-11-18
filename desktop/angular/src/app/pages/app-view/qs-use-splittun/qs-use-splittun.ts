import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, EventEmitter, Input, OnChanges, OnInit, Output, SimpleChanges, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { BoolSetting, ConfigService, Setting, StringArraySetting, getActualValue } from "@safing/portmaster-api";
import { SaveSettingEvent } from "src/app/shared/config/generic-setting/generic-setting";

const configKeys = {
  splitTunUse: 'splittun/use',
  splitTunEnable: 'splittun/enable',
  splitTunUsagePolicy: 'splittun/usagePolicy',
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

    if (!this.currentValue || !this.splitTunModuleEnabled) {
      return;
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
