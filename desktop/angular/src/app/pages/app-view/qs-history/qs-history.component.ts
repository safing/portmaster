import {
  ChangeDetectionStrategy,
  Component,
  EventEmitter,
  Input,
  OnChanges,
  Output,
  SimpleChanges,
} from '@angular/core';
import {
  BoolSetting,
  Setting,
  getActualValue,
} from '@safing/portmaster-api';
import { SaveSettingEvent } from 'src/app/shared/config';

@Component({
  selector: 'app-qs-history',
  templateUrl: './qs-history.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class QsHistoryComponent implements OnChanges {
  currentValue = false;

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
