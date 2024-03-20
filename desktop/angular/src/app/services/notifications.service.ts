import { HttpClient } from '@angular/common/http';
import { Injectable, TrackByFunction, inject } from '@angular/core';
import { Params, Router } from '@angular/router';
import { PortapiService, RetryableOpts } from '@safing/portmaster-api';
import { BehaviorSubject, Observable, combineLatest, defer, throwError } from 'rxjs';
import { map, share, toArray } from 'rxjs/operators';
import { environment } from 'src/environments/environment';
import { ActionIndicatorService } from '../shared/action-indicator';
import { Action, ActionHandler, NetqueryAction, Notification, NotificationState, NotificationType, OpenPageAction, OpenProfileAction, OpenSettingAction, OpenURLAction, PageIDs, WebhookAction } from './notifications.types';
import { VirtualNotification } from './virtual-notification';
import { INTEGRATION_SERVICE } from '../integration';

@Injectable({
  providedIn: 'root'
})
export class NotificationsService {
  private readonly integration = inject(INTEGRATION_SERVICE);

  /**
   * A {@link TrackByFunction} from tracking notifications.
   */
  static trackBy: TrackByFunction<Notification<any>> = function (_: number, n: Notification<any>) {
    return n.EventID;
  };

  /**
   * This object contains handler methods for all
   * notification action types we currently support.
   */
  private actionHandler: {
    [key in Action['Type']]: (a: any) => Promise<any>;
  } = {
      '': async () => { },
      'open-url': async (a: OpenURLAction) => {
        await this.integration.openExternal(a.Payload);
      },
      'open-profile': (a: OpenProfileAction) => this.router.navigate([
        '/app', ...a.Payload.split('/')
      ]),
      'open-setting': (a: OpenSettingAction) => {
        if (a.Payload.Profile) {
          return this.router.navigate(['/app', ...a.Payload.Profile.split('/')], {
            queryParams: {
              setting: a.Payload.Key,
              tab: 'settings'
            }
          })
        }
        return this.router.navigate(['/settings'], {
          queryParams: {
            setting: a.Payload.Key
          }
        })
      },
      "open-page": (a: OpenPageAction) => {
        let pageID: keyof typeof PageIDs | null = null;
        let queryParams: Params | null = null;

        if (typeof a.Payload === 'string') {
          pageID = a.Payload;
          queryParams = {};
        } else {
          pageID = a.Payload.id;
          queryParams = a.Payload.query;
        }

        const url = PageIDs[pageID];
        if (!!url) {
          return this.router.navigate([url], {
            queryParams,
          })
        }
        return Promise.reject('not yet supported');
      },
      "ui": (a: ActionHandler<any>) => {
        return a.Run(a);
      },
      "netquery": (a: NetqueryAction) => {
        return this.router.navigate(['/monitor'], {
          queryParams: {
            q: a.Payload,
          }
        })
      },
      "call-webhook": (a: WebhookAction) => {
        let method = a.Payload.Method;
        if (method === '') {
          if (a.Payload.Payload !== undefined && a.Payload.Payload !== null) {
            method = 'PUT'
          } else {
            method = 'POST'
          }
        }
        let req = this.http.request(
          method,
          `${environment.httpAPI}/v1/${a.Payload.URL}`,
          {
            body: a.Payload.Payload,
            observe: 'response',
            responseType: 'arraybuffer',
          }
        )
        return new Promise((resolve, reject) => {
          const observer = this.actionIndicator.httpObserver();
          req.subscribe({
            next: res => {
              if (a.Payload.ResultAction === 'display') {
                if (!!observer?.next) {
                  observer.next(res)
                }
              }
              resolve(res);
            },
            error: err => {
              if (!!observer?.error) {
                observer.error(err);
              }
              reject(err);
            },
          })
        })
      }
    };

  // For testing purposes only
  VirtualNotification = VirtualNotification;

  /** A map of virtual notifications */
  private _virtualNotifications = new Map<string, VirtualNotification<any>>();

  /* Emits all virtual notifications whenever they change */
  private _virtualNotificationChange = new BehaviorSubject<VirtualNotification<any>[]>([]);

  /* A copy of the static trackBy function. */
  trackBy = NotificationsService.trackBy;

  /** The prefix that all notifications have */
  readonly notificationPrefix = "notifications:all/";

  /** new$ emits new (active) notifications as they arrive */
  readonly new$: Observable<Notification<any>[]>;

  constructor(
    private portapi: PortapiService,
    private router: Router,
    private http: HttpClient,
    private actionIndicator: ActionIndicatorService,
  ) {
    this.new$ = this.watchAll().pipe(
      src => this.injectVirtual(src),
      map(msgs => {
        return msgs.filter(msg => msg.State === NotificationState.Active || !msg.State)
      }),
      share({ connector: () => new BehaviorSubject<Notification<any>[]>([]) })
    );
  }

  /**
   * Inject a new virtual notification. If not configured otherwise,
   * the notification is automatically removed when executed.
   */
  inject(notif: VirtualNotification<any>, { autoRemove } = { autoRemove: true }) {
    this._virtualNotifications.set(notif.EventID, notif);
    this._virtualNotificationChange.next(
      Array.from(this._virtualNotifications.values())
    )

    if (autoRemove) {
      notif.executed.subscribe({ complete: () => this.deject(notif) });
    }
  }

  /** Deject (remove) a virtual notification. */
  deject(notif: VirtualNotification<any>) {
    this._virtualNotifications.delete(notif.EventID);

    this._virtualNotificationChange.next(
      Array.from(this._virtualNotifications.values())
    )
  }

  /** A {@link MonoOperatorFunction} that injects all virtual observables into the source. */
  private injectVirtual(obs: Observable<Notification<any>[]>): Observable<Notification[]> {
    return combineLatest([
      obs,
      this._virtualNotificationChange,
    ]).pipe(
      map(([real, virtual]) => {
        return [
          ...real,
          ...virtual,
        ]
      })
    )
  }

  /**
   * Watch all notifications that match a query.
   *
   *
   * @param query The query to watch. Defaulta to all notifcations
   * @param opts Optional retry configuration options.
   */
  watchAll<T = any>(query: string = '', opts?: RetryableOpts): Observable<Notification<T>[]> {
    return this.portapi.watchAll<Notification<T>>(this.notificationPrefix + query, opts);
  }

  /**
   * Query the backend for a list of notifications. In contrast
   * to {@class PortAPI} query collects all results into an array
   * first which makes it convenient to be used in *ngFor and
   * friends. See {@function trackNotification} for a suitable track-by
   * function.
   *
   * @param query The search query.
   */
  query(query: string): Observable<Notification<any>[]> {
    return this.portapi.query<Notification<any>>(this.notificationPrefix + query)
      .pipe(
        map(value => value.data),
        toArray()
      )
  }

  /**
   * Returns the notification by ID.
   *
   * @param id The ID of the notification
   */
  get<T>(id: string): Observable<Notification<T>> {
    return this.portapi.get(this.notificationPrefix + id)
  }

  /**
   * Execute an action attached to a notification.
   *
   * @param n The notification object.
   * @param actionId The ID of the action to execute.
   */
  execute(n: Notification<any>, action: Action): Observable<void>;

  /**
   * Execute an action attached to a notification.
   *
   * @param notificationId The ID of the notification.
   * @param actionId The ID of the action to execute.
   */
  execute(notificationId: string, action: Action): Observable<void>;

  // overloaded implementation of execute
  execute(notifOrId: Notification<any> | string, action: Action): Observable<void> {
    const payload: Partial<Notification<any>> = {};
    if (typeof notifOrId === 'string') {
      payload.EventID = notifOrId;
    } else {
      payload.EventID = notifOrId.EventID;
    }

    // if it's a virtual notification we should let it handle the action
    // on it's own.
    if (!!this._virtualNotifications.get(payload.EventID)) {
      return defer(async () => {
        const notif = this._virtualNotifications.get(payload.EventID!);
        if (!!notif) {
          notif.selectAction(action.ID);
        }
      })
    }

    return defer(async () => {
      try {
        await this.performAction(action);

        // finally, if there's an action ID, mark the notification as resolved.
        if (!!action.ID) {
          payload.SelectedActionID = action.ID;
          const key = this.notificationPrefix + payload.EventID;
          await this.portapi.update(key, payload).toPromise();
        }
      } catch (err: any) {
        const msg = this.actionIndicator.getErrorMessgae(err);
        this.actionIndicator.error('Internal Error', 'Failed to perform action: ' + msg)
      }
    })
  }

  async performAction(action: Action) {
    // if there's an action type defined execute the handler.
    if (!!action.Type) {
      const handler = this.actionHandler[action.Type] as (a: Action) => Promise<any>;
      if (!!handler) {
        console.log(action);
        await handler(action);
      } else {
        this.actionIndicator.error('Internal Error', 'Cannot handle action type ' + action.Type)
      }
    }
  }

  /**
   * Resolve a pending notification execution.
   *
   * @param n The notification object to resolve the pending execution.
   * @param time optional The time at which the pending execution took place
   */
  resolvePending(n: Notification<any>, time?: number): Observable<void>;

  /**
   * Resolve a pending notification execution.
   *
   * @param n The notification ID to resolve the pending execution.
   * @param time optional The time at which the pending execution took place
   */
  resolvePending(n: string, time?: number): Observable<void>;

  // overloaded implementation of resolvePending.
  resolvePending(notifOrID: Notification<any> | string, time: number = (Math.round(Date.now() / 1000))): Observable<void> {
    const payload: Partial<Notification<any>> = {};
    if (typeof notifOrID === 'string') {
      payload.EventID = notifOrID;
    } else {
      payload.EventID = notifOrID.EventID;
      if (notifOrID.State === NotificationState.Executed) {
        return throwError(`Notification ${notifOrID.EventID} already executed`);
      }
    }

    payload.State = NotificationState.Responded;
    const key = this.notificationPrefix + payload.EventID
    return this.portapi.update(key, payload);
  }

  /**
   * Delete a notification.
   *
   * @param n The notification to delete.
   */
  delete(n: Notification<any>): Observable<void>;

  /**
   * Delete a notification.
   *
   * @param n The notification to delete.
   */
  delete(id: string): Observable<void>;

  // overloaded implementation of delete.
  delete(notifOrId: Notification<any> | string): Observable<void> {
    return this.portapi.delete(typeof notifOrId === 'string' ? notifOrId : notifOrId.EventID);
  }

  /**
   * Create a new notification.
   *
   * @param n The notification to create.
   */
  create(n: Partial<Notification<any>>): Observable<void>;

  /**
   * Create a new notification.
   *
   * @param id The ID of the notificaiton.
   * @param message The default message of the notificaiton.
   * @param type The notification type
   * @param args Additional arguments for the notification.
   */
  create(id: string, message: string, type: NotificationType, args?: Partial<Notification<any>>): Observable<void>;

  // overloaded implementation of create.
  create(notifOrId: Partial<Notification<any>> | string, message?: string, type?: NotificationType, args?: Partial<Notification<any>>): Observable<void> {
    if (typeof notifOrId === 'string') {
      notifOrId = {
        ...args,
        EventID: notifOrId,
        State: NotificationState.Active,
        Message: message,
        Type: type,
      } as Notification<any>; // it's actual Partial but that's fine.
    }

    if (!notifOrId.EventID) {
      return throwError(`Notification ID is required`);
    }

    if (!notifOrId.Message) {
      return throwError(`Notification message is required`);
    }

    if (typeof notifOrId.Type !== 'number') {
      return throwError(`Notification type is required`);
    }

    return this.portapi.create(this.notificationPrefix + notifOrId.EventID, notifOrId);
  }
}
