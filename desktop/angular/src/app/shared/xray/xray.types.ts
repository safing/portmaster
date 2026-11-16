/** A subscription URL that expands into a list of servers/countries. */
export interface XraySubscription {
  id: string;
  url: string;
  name?: string;
  country?: string;
  node_count?: number;
  added_at?: string;
}

/** Contract alias for consumers that prefer the short name. */
export type Subscription = XraySubscription;

/** A single outbound node parsed from a subscription. */
export interface XrayNode {
  id: string;
  name: string;
  /** ISO 3166-1 alpha-2 country code, e.g. "DE". */
  country: string;
  protocol: string;
  address: string;
  port: number;
  subscriptionId: string;
  /** Optional geo position; falls back to a country centroid on the map. */
  latitude?: number;
  longitude?: number;
}

/** Current state of the Xray connection. */
export interface XrayStatus {
  connected: boolean;
  nodeId?: string | null;
  node_name?: string | null;
  country?: string | null;
  since?: string | null;
  state?: string;
}

/** Contract alias for consumers that prefer the short name. */
export type Status = XrayStatus;
