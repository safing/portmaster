import { Injectable } from '@angular/core';
import {
  GeoCoordinates,
  IPProtocol,
  IPScope,
  IntelEntity,
  Pin,
  UnknownLocation,
} from '@safing/portmaster-api';
import { BehaviorSubject, Observable, map, of, share } from 'rxjs';
import { MapPin } from '../spn/map.service';
import { XrayNode, XrayStatus } from '../../shared/xray';
import { countryCenter } from './country-centers';

/**
 * A tiny, component-scoped replacement for the SPN MapService. It exposes the
 * same pin observables so the existing SPN map overlays (pin-list,
 * country-overlay) can be reused unchanged with subscription nodes.
 */
@Injectable()
export class XrayMapService {
  readonly _pins$ = new BehaviorSubject<MapPin[]>([]);

  get pins$(): Observable<MapPin[]> {
    return this._pins$.asObservable();
  }

  readonly pinsMap$ = this.pins$.pipe(
    map(allPins => {
      const lookup = new Map<string, MapPin>();
      allPins.forEach(pin => lookup.set(pin.pin.ID, pin));
      return lookup;
    }),
    share(),
  );

  /** Placeholder status so SPN-specific overlays stay happy. */
  readonly status$: Observable<'connected'> = of('connected');
  readonly activeSince$: Observable<string | null> = of(null);

  setPins(pins: MapPin[]): void {
    this._pins$.next(pins);
  }

  /** Convert one backend node into the MapPin shape used by the SPN map. */
  toMapPin(node: XrayNode, status: XrayStatus | null): MapPin {
    const centered = countryCenter(node.country);
    const location: GeoCoordinates =
      typeof node.latitude === 'number' && typeof node.longitude === 'number'
        ? { AccuracyRadius: 0, Latitude: node.latitude, Longitude: node.longitude }
        : centered
          ? { AccuracyRadius: 0, Latitude: centered[0], Longitude: centered[1] }
          : UnknownLocation;

    const entity: IntelEntity = {
      Protocol: IPProtocol.TCP,
      Port: node.port,
      Domain: node.address,
      CNAME: null,
      IP: node.address,
      IPScope: IPScope.Global,
      Country: node.country || '',
      ASN: 0,
      ASOrg: '',
      Coordinates: location,
      BlockedByLists: null,
      BlockedEntities: null,
      ListOccurences: null,
    };

    const pin: Pin = {
      ID: node.id,
      Name: node.name || node.address,
      FirstSeen: '',
      EntityV4: entity,
      EntityV6: null,
      States: ['Reachable'],
      SessionActive: false,
      HopDistance: 1,
      ConnectedTo: {},
      Route: null,
      VerifiedOwner: node.protocol || '',
    };

    const isExit = !!status && status.connected && status.nodeId === node.id;

    return {
      pin,
      location,
      entity,
      isOffline: false,
      isExit,
      isTransit: false,
      isActive: isExit,
      isHome: false,
      hasIssues: false,
    };
  }
}
