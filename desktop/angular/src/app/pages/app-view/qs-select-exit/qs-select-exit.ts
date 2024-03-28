import {
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  DestroyRef,
  EventEmitter,
  Input,
  OnChanges,
  OnInit,
  Output,
  SimpleChanges,
  inject,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import {
  BoolSetting,
  StringArraySetting,
  CountrySelectionQuickSetting,
  ConfigService,
  Setting,
  getActualValue,
} from '@safing/portmaster-api';
import { SaveSettingEvent } from 'src/app/shared/config/generic-setting/generic-setting';

@Component({
  selector: 'app-qs-select-exit',
  templateUrl: './qs-select-exit.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class QuickSettingSelectExitButtonComponent
  implements OnInit, OnChanges
{
  private destroyRef = inject(DestroyRef);

  @Input()
  canUse: boolean = true;

  @Input()
  settings: Setting[] = [];

  @Output()
  save = new EventEmitter<SaveSettingEvent>();

  spnEnabled: boolean | null = null;
  exitRuleSetting: StringArraySetting | null = null;

  selectedExitRules: string | undefined = undefined;
  availableExitRules: CountrySelectionQuickSetting<string[]>[] | null = null;

  constructor(
    private configService: ConfigService,
    private cdr: ChangeDetectorRef
  ) {}

  updateExitRules(newExitRules: string) {
    this.selectedExitRules = newExitRules;

    let newConfigValue: string[] = [];
    if (!!newExitRules) {
      newConfigValue = newExitRules.split(',');
    }

    this.save.next({
      isDefault: false,
      key: 'spn/exitHubPolicy',
      value: newConfigValue,
    });
  }

  ngOnChanges(changes: SimpleChanges): void {
    if ('settings' in changes) {
      this.exitRuleSetting = null;
      this.selectedExitRules = undefined;

      const exitRuleSetting = this.settings.find(
        (s) => s.Key == 'spn/exitHubPolicy'
      ) as StringArraySetting | undefined;
      if (exitRuleSetting) {
        this.exitRuleSetting = exitRuleSetting;
        this.updateOptions();
      }
    }
  }

  ngOnInit() {
    this.configService
      .watch<BoolSetting>('spn/enable')
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe((value) => {
        this.spnEnabled = value;
        this.updateOptions();
      });
  }

  private updateOptions() {
    if (!this.exitRuleSetting) {
      this.selectedExitRules = undefined;
      this.availableExitRules = null;
      return;
    }

    if (!!this.exitRuleSetting.Value && this.exitRuleSetting.Value.length > 0) {
      this.selectedExitRules = this.exitRuleSetting.Value.join(',');
    }
    this.availableExitRules = this.getQuickSettings();

    this.cdr.markForCheck();
  }

  private getQuickSettings(): CountrySelectionQuickSetting<string[]>[] {
    if (!this.exitRuleSetting) {
      return [];
    }

    let val = this.exitRuleSetting.Annotations[
      'safing/portbase:ui:quick-setting'
    ] as CountrySelectionQuickSetting<string[]>[];
    if (val === undefined) {
      return [];
    }

    if (!Array.isArray(val)) {
      return [];
    }

    return val;
  }
}
