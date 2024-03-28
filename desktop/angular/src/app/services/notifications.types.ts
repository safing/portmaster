import { getEnumKey, IntelEntity, Record } from '@safing/portmaster-api';

/**
 * BaseAction defines a user selectable action and can
 * be attached to a notification. Once selected,
 * the action's ID is set as the SelectedActionID
 * of the notification.
 */
export interface BaseAction {
  // ID uniquely identifies the action. It's safe to
  // use ID to select a localizable template to use
  // instead of the Text property. If Type is set
  // to None the ID may be empty, signifying that this
  // action is merely to dismiss the notification.
  ID: string;
  // Text is the (default) text for the action label.
  Text: string;
}

export interface GenericAction extends BaseAction {
  Type: '';
}

export interface OpenURLAction extends BaseAction {
  Type: 'open-url';
  Payload: string;
}

export interface OpenPageAction extends BaseAction {
  Type: 'open-page';
  Payload: keyof typeof PageIDs | {
    id: keyof typeof PageIDs,
    query: {
      [key: string]: string,
    }
  };
}

export interface NetqueryAction extends BaseAction {
  Type: 'netquery';
  Payload: string;
}

/**
 * PageIDs holds a list of pages that can be opened using
 * the OpenPageAction.
 */
export const PageIDs = {
  'monitor': '/monitor',
  'support': '/support',
  'settings': '/settings',
  'apps': '/app/overview',
  'spn': '/spn',
}

export interface OpenSettingAction extends BaseAction {
  Type: 'open-setting';
  Payload: {
    Key: string;
    Profile?: string;
  }
}

export interface OpenProfileAction extends BaseAction {
  Type: 'open-profile';
  Payload: string;
}

export interface WebhookAction extends BaseAction {
  Type: 'call-webhook';
  Payload: {
    Method: string;
    URL: string;
    Payload: any;
    ResultAction: 'ignore' | 'display';
  }
}

export interface ActionHandler<T> extends BaseAction {
  Type: 'ui'
  Run: (vn: T) => Promise<void>;
  Payload: T;
}

export type Action = GenericAction
  | OpenURLAction
  | OpenPageAction
  | OpenSettingAction
  | OpenProfileAction
  | WebhookAction
  | NetqueryAction
  | ActionHandler<any>;

/** All action types that perform in-application routing. */
export const routingActions = new Set<Action['Type']>([
  'open-page',
  'open-profile',
  'open-setting'
])

/**
 * Available types of notifications. Notification
 * types are mainly for filtering and style related
 * decisions.
 */
export enum NotificationType {
  // Info is an informational message only.
  Info = 0,
  // Warning is a warning message.
  Warning = 1,
  // Prompt asks the user for a decision.
  Prompt = 2,
  // Error is for error notifications and module
  // failure status.
  Error = 3,
}

export interface ConnectionPromptData {
  Profile: {
    ID: string;
    LinkedPath: string;
    Source: 'local';
  };
  Entity: IntelEntity;
}

/**
 * Returns a string representation of the notifcation type.
 *
 * @param val The notifcation type
 */
export function getNotificationTypeString(val: NotificationType): string {
  return getEnumKey(NotificationType, val)
}

/**
 * Each notification can be in one of six different states
 * that inform the client on how to handle the notification.
 */
export enum NotificationState {
  // Active describes a notification that is active, no expired and,
  // if actions are available, still waits for the user to select an
  // action.
  Active = "active",
  // Responded describes a notification where the user has already
  // selected which action to take but that action is still to be
  // performed.
  Responded = "responded",
  // Responded describes a notification where the user has already
  // selected which action to take but that action is still to be
  // performed.
  Executed = "executed",
  // Invalid is a UI-only state that is used when the state of a
  // notification is unknown.
  Invalid = "invalid",
}

export interface Notification<T = any> extends Record {
  // EventID is used to identify a specific notification. It consists of
  // the module name and a per-module unique event id.
  // The following format is recommended:
  // 	<module-id>:<event-id>
  EventID: string;
  // GUID is a unique identifier for each notification instance. That is
  // two notifications with the same EventID must still have unique GUIDs.
  // The GUID is mainly used for system (Windows) integration and is
  // automatically populated by the notification package. Average users
  // don't need to care about this field.
  GUID: string;
  // Type is the notification type. It can be one of Info, Warning or Prompt.
  Type: NotificationType;
  // Message is the default message shown to the user if no localized version
  // of the notification is available. Note that the message should already
  // have any paramerized values replaced. Message may be formatted using
  // markdown.
  Message: string;
  // Title holds a short notification title that quickly informs the user
  // about the type of notification.
  Title: string;
  // Category holds an informative category for the notification and is mainly
  // used for presentation purposes.
  Category: string;
  // EventData contains an additional payload for the notification. This payload
  // may contain contextual data and may be used by a localization framework
  // to populate the notification message template.
  // If EventData implements sync.Locker it will be locked and unlocked together with the
  // notification. Otherwise, EventData is expected to be immutable once the
  // notification has been saved and handed over to the notification or database package.
  EventData: T | null;
  // Expires holds the unix epoch timestamp at which the notification expires
  // and can be cleaned up.
  // Users can safely ignore expired notifications and should handle expiry the
  // same as deletion.
  Expires: number;
  // State describes the current state of a notification. See State for
  // a list of available values and their meaning.
  State: NotificationState;
  // AvailableActions defines a list of actions that a user can choose from.
  AvailableActions: Action[];
  // SelectedActionID is updated to match the ID of one of the AvailableActions
  // based on the user selection.
  SelectedActionID: string;
}

export type ConnectionPrompt = Notification<ConnectionPromptData>;
