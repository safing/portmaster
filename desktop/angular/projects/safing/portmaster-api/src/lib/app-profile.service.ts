import { HttpClient, HttpHeaders, HttpParams } from '@angular/common/http';
import { Inject, Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { filter, finalize, map, mergeMap, share, take } from 'rxjs/operators';
import {
  AppProfile,
  FlatConfigObject,
  LayeredProfile,
  TagDescription,
  flattenProfileConfig,
} from './app-profile.types';
import {
  PORTMASTER_HTTP_API_ENDPOINT,
  PortapiService,
} from './portapi.service';
import { Process } from './portapi.types';

@Injectable({
  providedIn: 'root',
})
export class AppProfileService {
  private watchedProfiles = new Map<string, Observable<AppProfile>>();

  constructor(
    private portapi: PortapiService,
    private http: HttpClient,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) private httpAPI: string
  ) { }

  /**
   * Returns the database key of a profile.
   *
   * @param source The source of the profile.
   * @param id The profile ID.
   */
  getKey(source: string, id: string): string;

  /**
   * Returns the database key of a profile
   *
   * @param p The app-profile itself..
   */
  getKey(p: AppProfile): string;

  getKey(idOrSourceOrProfile: string | AppProfile, id?: string): string {
    if (typeof idOrSourceOrProfile === 'object') {
      return this.getKey(idOrSourceOrProfile.Source, idOrSourceOrProfile.ID);
    }

    let key = idOrSourceOrProfile;

    if (!!id) {
      key = `core:profiles/${idOrSourceOrProfile}/${id}`;
    }

    return key;
  }

  /**
   * Load an application profile.
   *
   * @param sourceAndId The full profile ID including source
   */
  getAppProfile(sourceAndId: string): Observable<AppProfile>;

  /**
   * Load an application profile.
   *
   * @param source The source of the profile
   * @param id The ID of the profile
   */
  getAppProfile(source: string, id: string): Observable<AppProfile>;

  getAppProfile(
    sourceOrSourceAndID: string,
    id?: string
  ): Observable<AppProfile> {
    let source = sourceOrSourceAndID;
    if (id !== undefined) {
      source += '/' + id;
    }
    const key = `core:profiles/${source}`;

    if (this.watchedProfiles.has(key)) {
      return this.watchedProfiles.get(key)!.pipe(take(1));
    }

    return this.getAppProfileFromKey(key);
  }

  setProfileIcon(
    content: string | ArrayBuffer,
    mimeType: string
  ): Observable<{ filename: string }> {
    return this.http.post<{ filename: string }>(
      `${this.httpAPI}/v1/profile/icon`,
      content,
      {
        headers: new HttpHeaders({
          'Content-Type': mimeType,
        }),
      }
    );
  }

  /**
   * Loads an application profile by it's database key.
   *
   * @param key The key of the application profile.
   */
  getAppProfileFromKey(key: string): Observable<AppProfile> {
    return this.portapi.get(key);
  }

  /**
   * Loads the global-configuration profile.
   */
  globalConfig(): Observable<FlatConfigObject> {
    return this.getAppProfile('special', 'global-config').pipe(
      map((profile) => flattenProfileConfig(profile.Config))
    );
  }

  /** Returns all possible process tags. */
  tagDescriptions(): Observable<TagDescription[]> {
    return this.http
      .get<{ Tags: TagDescription[] }>(`${this.httpAPI}/v1/process/tags`)
      .pipe(map((result) => result.Tags));
  }

  /**
   * Watches an application profile for changes.
   *
   * @param source The source of the profile
   * @param id The ID of the profile
   */
  watchAppProfile(sourceAndId: string): Observable<AppProfile>;
  /**
   * Watches an application profile for changes.
   *
   * @param source The source of the profile
   * @param id The ID of the profile
   */
  watchAppProfile(source: string, id: string): Observable<AppProfile>;

  watchAppProfile(sourceAndId: string, id?: string): Observable<AppProfile> {
    let key = '';

    if (id === undefined) {
      key = sourceAndId;
      if (!key.startsWith('core:profiles/')) {
        key = `core:profiles/${key}`;
      }
    } else {
      key = `core:profiles/${sourceAndId}/${id}`;
    }

    if (this.watchedProfiles.has(key)) {
      return this.watchedProfiles.get(key)!;
    }

    const stream = this.portapi.get<AppProfile>(key).pipe(
      mergeMap(() => this.portapi.watch<AppProfile>(key)),
      finalize(() => {
        console.log(
          'watchAppProfile: removing cached profile stream for ' + key
        );
        this.watchedProfiles.delete(key);
      }),
      share({
        connector: () => new BehaviorSubject<AppProfile | null>(null),
        resetOnRefCountZero: true,
      }),
      filter((profile) => profile !== null)
    ) as Observable<AppProfile>;

    this.watchedProfiles.set(key, stream);

    return stream;
  }

  /** @deprecated use saveProfile instead */
  saveLocalProfile(profile: AppProfile): Observable<void> {
    return this.saveProfile(profile);
  }

  /**
   * Save an application profile.
   *
   * @param profile The profile to save
   */
  saveProfile(profile: AppProfile): Observable<void> {
    profile.LastEdited = Math.floor(new Date().getTime() / 1000);
    return this.portapi.update(
      `core:profiles/${profile.Source}/${profile.ID}`,
      profile
    );
  }

  /**
   * Watch all application profiles
   */
  watchProfiles(): Observable<AppProfile[]> {
    return this.portapi.watchAll<AppProfile>('core:profiles/');
  }

  watchLayeredProfile(source: string, id: string): Observable<LayeredProfile>;

  /**
   * Watches the layered runtime profile for a given application
   * profile.
   *
   * @param profile The app profile
   */
  watchLayeredProfile(profile: AppProfile): Observable<LayeredProfile>;

  watchLayeredProfile(
    profileOrSource: string | AppProfile,
    id?: string
  ): Observable<LayeredProfile> {
    if (typeof profileOrSource == 'object') {
      id = profileOrSource.ID;
      profileOrSource = profileOrSource.Source;
    }

    const key = `runtime:layeredProfile/${profileOrSource}/${id}`;
    return this.portapi.watch<LayeredProfile>(key);
  }

  /**
   * Loads the layered runtime profile for a given application
   * profile.
   *
   * @param profile The app profile
   */
  getLayeredProfile(profile: AppProfile): Observable<LayeredProfile> {
    const key = `runtime:layeredProfile/${profile.Source}/${profile.ID}`;
    return this.portapi.get<LayeredProfile>(key);
  }

  /**
   * Delete an application profile.
   *
   * @param profile The profile to delete
   */
  deleteProfile(profile: AppProfile): Observable<void> {
    return this.portapi.delete(`core:profiles/${profile.Source}/${profile.ID}`);
  }

  getProcessesByProfile(profileOrId: AppProfile | string): Observable<Process[]> {
    if (typeof profileOrId === 'object') {
      profileOrId = profileOrId.Source + "/" + profileOrId.ID
    }

    return this.http.get<Process[]>(`${this.httpAPI}/v1/process/list/by-profile/${profileOrId}`)
  }

  getProcessByPid(pid: number): Observable<Process> {
    return this.http.get<Process>(`${this.httpAPI}/v1/process/group-leader/${pid}`)
  }
}

