import {
  ChangeDetectionStrategy,
  Component,
  EventEmitter,
  Input,
  OnChanges,
  Output,
  SimpleChanges,
  inject,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import {
  BoolSetting,
  FeatureID,
  SPNService,
  Setting,
  getActualValue,
} from '@safing/portmaster-api';
import { BehaviorSubject, Observable, map } from 'rxjs';
import { share } from 'rxjs/operators';
import { SaveSettingEvent } from 'src/app/shared/config';

@Component({
  selector: 'app-qs-history',
  templateUrl: './qs-history.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class QsHistoryComponent implements OnChanges {
  currentValue = false;
  historyFeatureAllowed: Observable<boolean> = inject(SPNService).profile$.pipe(
    takeUntilDestroyed(),
    map((profile) => {
      return (
        profile?.current_plan?.feature_ids?.includes(FeatureID.History) || false
      );
    }),
    share({ connector: () => new BehaviorSubject<boolean>(false) })
  );

  @Input()
  canUse: boolean = true;

  @Input()
  settings: Setting[] = [];

  @Output()
  save = new EventEmitter<SaveSettingEvent<any>>();

  ngOnChanges(changes: SimpleChanges): void {
    if ('settings' in changes) {
      const historySetting = this.settings.find(
        (s) => s.Key === 'history/enable'
      ) as BoolSetting | undefined;
      if (historySetting) {
        this.currentValue = getActualValue(historySetting);
      }
    }
  }

  updateHistoryEnabled(enabled: boolean) {
    this.save.next({
      isDefault: false,
      key: 'history/enable',
      value: enabled,
    });
  }
}
