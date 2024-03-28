import { Request } from "./tab-tracker";

export interface ListRequests {
  type: 'listRequests';
  domain?: string;
  tabId: number | 'current';
}

export interface NotifyRequests {
  type: 'notifyRequests',
  requests: Request[];
}

export type CallRequest = ListRequests;
