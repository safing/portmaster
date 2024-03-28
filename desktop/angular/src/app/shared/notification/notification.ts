import { coerceBooleanProperty } from '@angular/cdk/coercion';
import { ChangeDetectionStrategy, Component, EventEmitter, HostBinding, Input, OnInit, Output, inject } from '@angular/core';
import { SFNG_DIALOG_REF } from '@safing/ui';
import { Action, NotificationState, NotificationsService, getNotificationTypeString } from '../../services';
import { _Notification } from '../notification-list/notification-list.component';

@Component({
  selector: 'app-notification',
  templateUrl: './notification.html',
  styleUrls: ['./notification.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class NotificationComponent implements OnInit {
  readonly ref = inject(SFNG_DIALOG_REF);
  readonly notification: _Notification<any> = inject(SFNG_DIALOG_REF).data;

  /**
   * The host tag of the notification component has the notification type
   * and the notification state as a class name set.
   * Examples:
   *
   *    notif-action-required notif-prompt
   */
  @HostBinding('class')
  get hostClass(): string {
    let cls = `notif-${this.state}`;
    if (!!this.notification) {
      cls = `${cls} notif-${getNotificationTypeString(this.notification.Type)}`
    }
    return cls
  }

  state: NotificationState = NotificationState.Invalid;

  ngOnInit() {
    if (!!this.notification) {
      this.state = this.notification.State || NotificationState.Invalid;
    } else {
      this.state = NotificationState.Invalid;
    }
  }

  @Input()
  set allowMarkdown(v: any) {
    this._markdown = coerceBooleanProperty(v);
  }
  get allowMarkdown() { return this._markdown; }
  private _markdown: boolean = true;

  @Output()
  actionExecuted: EventEmitter<Action> = new EventEmitter();

  constructor(private notifService: NotificationsService) { }

  execute(n: _Notification<any>, action: Action) {
    this.notifService.execute(n, action)
      .subscribe(
        () => {
          this.actionExecuted.next(action)
          this.ref.close();
        },
        err => console.error(err),
      )
  }
}
