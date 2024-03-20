import { animate, style, transition, trigger } from '@angular/animations';
import { ChangeDetectionStrategy, ChangeDetectorRef, Component, ElementRef, HostBinding, OnDestroy, OnInit, TrackByFunction, inject } from '@angular/core';
import { SfngDialogService } from '@safing/ui';
import { Subscription } from 'rxjs';
import { map } from 'rxjs/operators';
import { Action, Notification, NotificationType, NotificationsService } from 'src/app/services';
import { moveInOutAnimation, moveInOutListAnimation } from 'src/app/shared/animations';
import { NotificationComponent } from '../notification/notification';

export interface NotificationWidgetConfig {
  markdown: boolean;
}

export interface _Notification<T = any> extends Notification<T> {
  isBroadcast: boolean
}

@Component({
  selector: 'app-notification-list',
  templateUrl: './notification-list.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrls: [
    './notification-list.component.scss'
  ],
  animations: [
    trigger(
      'fadeIn',
      [
        transition(
          ':enter',
          [
            style({ opacity: 0 }),
            animate('.2s .2s ease-in',
              style({ opacity: 1 }))
          ]
        ),
      ]
    ),
    moveInOutAnimation,
    moveInOutListAnimation
  ]
})
export class NotificationListComponent implements OnInit, OnDestroy {
  readonly types = NotificationType;
  readonly dialog = inject(SfngDialogService);
  readonly cdr = inject(ChangeDetectorRef);

  /** Used to set a fixed height when a notification is expanded. */
  @HostBinding('style.height')
  height: null | string = null;

  /** Sets the overflow to hidden when a notification is expanded. */
  @HostBinding('style.overflow')
  get overflow() {
    if (this.height === null) {
      return null;
    }
    return 'hidden';
  }

  @HostBinding('class.empty')
  get isEmpty() {
    return this.notifications.length === 0;
  }

  @HostBinding('@moveInOutList')
  get length() { return this.notifications.length }

  /** Subscription to notification updates. */
  private notifSub = Subscription.EMPTY;

  /** All active notifications. */
  notifications: _Notification<any>[] = [];

  trackBy: TrackByFunction<_Notification> = this.notifsService.trackBy;

  constructor(
    public elementRef: ElementRef,
    public notifsService: NotificationsService,
  ) { }

  ngOnInit(): void {
    this.notifSub = this.notifsService
      .new$
      .pipe(
        // filter out any prompts as they are handled by a different widget.
        map(notifs => {
          return notifs.filter(notif => !notif.SelectedActionID && !(notif.Type === NotificationType.Prompt && notif.EventID.startsWith("filter:prompt")))
        })
      )
      .subscribe(list => {
        this.notifications = list.map(notification => {
          return {
            ...notification,
            isBroadcast: notification.EventID.startsWith("broadcasts:"),
          }
        });

        this.cdr.markForCheck();
      });
  }

  ngOnDestroy() {
    this.notifSub.unsubscribe();
  }

  /**
   * @private
   *
   * Executes a notification action and updates the "expanded-notification"
   * view if required.
   *
   * @param n  The notification object.
   * @param actionId  The ID of the action to execute.
   * @param event The mouse click event.
   */
  execute(n: _Notification<any>, action: Action, event: MouseEvent) {
    event.preventDefault();
    event.stopPropagation();

    this.notifsService.execute(n, action)
      .subscribe()
  }

  /**
   * @private
   * Toggles between list mode and notification-view mode.
   *
   * @param notif The notification that has been clicked.
   */
  toggelView(notif: _Notification<any>) {
    const ref = this.dialog.create(NotificationComponent, {
      backdrop: 'light',
      autoclose: true,
      data: notif,
    });
  }
}
