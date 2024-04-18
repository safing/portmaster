import { RecordMeta } from '@safing/portmaster-api';
import { BehaviorSubject } from 'rxjs';
import { filter } from 'rxjs/operators';
import { ActionHandler, Notification, NotificationState, NotificationType } from './notifications.types';

export class VirtualNotification<T> implements Notification<T> {
  readonly AvailableActions: ActionHandler<T>[];
  readonly Category: string;
  readonly EventData: T | null;
  readonly GUID: string = ''; // TODO(ppacher): should we fake it?
  readonly Expires: number;
  readonly _meta: RecordMeta;

  get State() {
    if (this.SelectedActionID === '') {
      return NotificationState.Active
    }

    return NotificationState.Executed
  }

  get SelectedActionID() {
    return this._selectedAction.getValue();
  }

  /** Emits as soon as the user selects one of the notification actions. */
  get executed() {
    return this._selectedAction.pipe(
      filter(action => action !== '')
    );
  }

  /* Used to emit the selected action */
  private _selectedAction = new BehaviorSubject<string>('');

  /**
   * Select and execute the action by ID.
   *
   * @param aid The ID of the action to execute.
   */
  selectAction(aid: string) {
    this._selectedAction.next(aid);
    this._meta.Modified = new Date().valueOf() / 1000;

    const action = this.AvailableActions.find(a => a.ID === aid);
    if (!!action) {
      action.Run(action.Payload);
    }
  }

  constructor(
    public readonly EventID: string,
    public readonly Type: NotificationType,
    public readonly Title: string,
    public readonly Message: string,
    {
      AvailableActions,
      EventData,
      Category,
      Expires,
    }: {
      AvailableActions?: ActionHandler<T>[];
      EventData?: T | null;
      Category?: string,
      Expires?: number,
    } = {}
  ) {
    this.AvailableActions = AvailableActions || [];
    this.EventData = EventData || null;
    this.Category = Category || '';
    this.Expires = Expires || 0;

    this._meta = {
      Created: new Date().valueOf() / 1000,
      Deleted: 0,
      Expires: this.Expires,
      Modified: new Date().valueOf() / 1000,
      Key: `notifications:all/${EventID}`,
    }
  }

  dispose() {
    this._selectedAction.complete();
  }
}
