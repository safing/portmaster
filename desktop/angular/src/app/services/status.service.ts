import { Injectable, TrackByFunction } from '@angular/core';
import { PortapiService, RetryableOpts, SecurityLevel, WatchOpts, trackById } from '@safing/portmaster-api';
import { BehaviorSubject, Observable } from 'rxjs';
import { filter, map, repeat, share, toArray } from 'rxjs/operators';
import { CoreStatus, Subsystem, VersionStatus } from './status.types';

@Injectable({
  providedIn: 'root'
})
export class StatusService {
  /**
   * A {@link TrackByFunction} from tracking subsystems.
   */
  static trackSubsystem: TrackByFunction<Subsystem> = trackById;
  readonly trackSubsystem = StatusService.trackSubsystem;
  
  /**
   * status$ watches the global core status. It's mutlicasted using a BehaviorSubject so new
   * subscribers will automatically get the latest version while only one subscription
   * to the backend is held.
   */
  readonly status$: Observable<CoreStatus> = this.portapi.qsub<CoreStatus>(`runtime:system/status`)
    .pipe(
      repeat({ delay: 2000 }),
      map(reply => reply.data),
      share({ connector: () => new BehaviorSubject<CoreStatus | null>(null) }),
      filter(value => value !== null),
    ) as Observable<CoreStatus>; // we filtered out the null values but we cannot make that typed with RxJS.

  constructor(private portapi: PortapiService) { }

  /** Returns the currently available versions for all resources. */
  getVersions(): Observable<VersionStatus> {
    return this.portapi.get<VersionStatus>('core:status/versions')
  }

  /**
   * Selectes a new security level. SecurityLevel.Off means that
   * the auto-pilot should take over.
   *
   * @param securityLevel The security level to select
   */
  selectLevel(securityLevel: SecurityLevel): Observable<void> {
    return this.portapi.update(`runtime:system/security-level`, {
      SelectedSecurityLevel: securityLevel,
    });
  }
}
