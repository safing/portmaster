import { FeatureID } from './features';
import { CountryInfo, GeoCoordinates, IntelEntity } from './network.types';
import { Record } from './portapi.types';

export interface SPNStatus extends Record {
  Status: 'failed' | 'disabled' | 'connecting' | 'connected';
  HomeHubID: string;
  HomeHubName: string;
  ConnectedIP: string;
  ConnectedTransport: string;
  ConnectedCountry: CountryInfo | null;
  ConnectedSince: string | null;
}

export interface Pin extends Record {
  ID: string;
  Name: string;
  FirstSeen: string;
  EntityV4?: IntelEntity | null;
  EntityV6?: IntelEntity | null;
  States: string[];
  SessionActive: boolean;
  HopDistance: number;
  ConnectedTo: {
    [key: string]: Lane,
  };
  Route: string[] | null;
  VerifiedOwner: string;
}

export interface Lane {
  HubID: string;
  Capacity: number;
  Latency: number;
}

export function getPinCoords(p: Pin): GeoCoordinates | null {
  if (p.EntityV4 && p.EntityV4.Coordinates) {
    return p.EntityV4.Coordinates;
  }
  return p.EntityV6?.Coordinates || null;
}

export interface Device {
  name: string;
  id: string;
}

export interface Subscription {
  ends_at: string;
  state: 'manual' | 'active' | 'cancelled';
  next_billing_date: string;
  payment_provider: string;
}

export interface Plan {
  name: string;
  amount: number;
  months: number;
  renewable: boolean;
  feature_ids: FeatureID[];
}

export interface View {
  Message: string;
  ShowAccountData: boolean;
  ShowAccountButton: boolean;
  ShowLoginButton: boolean;
  ShowRefreshButton: boolean;
  ShowLogoutButton: boolean;
}

export interface UserProfile extends Record {
  username: string;
  state: string;
  balance: number;
  device: Device | null;
  subscription: Subscription | null;
  current_plan: Plan | null;
  next_plan: Plan | null;
  view: View | null;
  LastNotifiedOfEnd?: string;
  LoggedInAt?: string;
}

export interface Package {
  Name: string;
  HexColor: string;
}

export interface Feature {
  ID: string;
  Name: string;
  ConfigKey: string;
  ConfigScope: string;
  RequiredFeatureID: FeatureID;
  InPackage: Package | null;
  Comment: string;
  Beta?: boolean;
  ComingSoon?: boolean;

  // does not come from the PM API but is set by SPNService
  IconURL: string;
}
