import { ChangeDetectionStrategy, ChangeDetectorRef, Component, DestroyRef, EventEmitter, Input, OnChanges, OnInit, Output, SimpleChanges, inject } from "@angular/core";
import { takeUntilDestroyed } from "@angular/core/rxjs-interop";
import { BoolSetting, ConfigService, Setting, getActualValue } from "@safing/portmaster-api";
import { SaveSettingEvent } from "src/app/shared/config/generic-setting/generic-setting";

const interferingSettingsWhenOn = [
  'spn/usagePolicy'
]

@Component({
  selector: 'app-qs-use-spn',
  templateUrl: './qs-use-spn.html',
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class QuickSettingUseSPNButtonComponent implements OnInit, OnChanges {
  private destroyRef = inject(DestroyRef);

  @Input()
  canUse: boolean = true;

  @Input()
  settings: Setting[] = [];

  @Output()
  save = new EventEmitter<SaveSettingEvent>();

  currentValue = false

  interferingSettings: Setting[] = [];

  /* Whether or not the SPN is currently disabled. null means we don't know yet ... */
  spnDisabled: boolean | null = null;

  constructor(
    private configService: ConfigService,
    private cdr: ChangeDetectorRef
  ) { }

  ngOnChanges(changes: SimpleChanges): void {
    if ('settings' in changes) {
      this.currentValue = false;

      const useSpnSetting = this.settings.find(s => s.Key === 'spn/use') as (BoolSetting | undefined);
      if (!!useSpnSetting) {
        this.currentValue = getActualValue(useSpnSetting);
        this.updateInterfering();
      }
    }
  }

  updateUseSpn(allowed: boolean) {
    this.save.next({
      isDefault: false,
      key: 'spn/use',
      value: allowed,
    })
  }

  ngOnInit() {
    this.configService.watch<BoolSetting>('spn/enable')
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(value => {
        this.spnDisabled = !value;
        this.cdr.markForCheck();
        this.updateInterfering();
      })
  }

  private updateInterfering() {
    this.interferingSettings = [];

    // only "enabled" state has interfering settings
    // only show if we already know if the SPN module is enabled
    if (!this.currentValue || this.spnDisabled !== false) {
      return
    }

    // create a lookup map for setting key to setting
    const lm = new Map<string, Setting>();
    this.settings.forEach(s => lm.set(s.Key, s))


    this.interferingSettings = interferingSettingsWhenOn
      .map(key => lm.get(key))
      .filter(setting => {
        if (!setting) {
          return false;
        }
        const value = getActualValue(setting);
        if (Array.isArray(value)) {
          return value.length > 0;
        }

        return !!value;
      }) as Setting[];
  }
}
