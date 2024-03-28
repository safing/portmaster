import { Injectable, TrackByFunction } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { distinctUntilChanged, filter, map, share, toArray } from 'rxjs/operators';
import { BaseSetting, BoolSetting, OptionType, Setting, SettingValueType } from './config.types';
import { PortapiService } from './portapi.service';


@Injectable({
  providedIn: 'root'
})
export class ConfigService {
  networkRatingEnabled$: Observable<boolean>;

  /**
   * A {@link TrackByFunction} for tracking settings.
   */
  static trackBy: TrackByFunction<Setting> = (_: number, obj: Setting) => obj.Name;
  readonly trackBy = ConfigService.trackBy;

  /** configPrefix is the database key prefix for the config db */
  readonly configPrefix = "config:";

  constructor(private portapi: PortapiService) {
    this.networkRatingEnabled$ = this.watch<BoolSetting>("core/enableNetworkRating")
      .pipe(
        share({ connector: () => new BehaviorSubject(false) }),
      )
  }

  /**
   * Loads a configuration setting from the database.
   *
   * @param key The key of the configuration setting.
   */
  get(key: string): Observable<Setting> {
    return this.portapi.get<Setting>(this.configPrefix + key);
  }

  /**
   * Returns all configuration settings that match query. Note that in
   * contrast to {@link PortAPI} settings values are collected into
   * an array before being emitted. This allows simple usage in *ngFor
   * and friends.
   *
   * @param query The query used to search for configuration settings.
   */
  query(query: string): Observable<Setting[]> {
    return this.portapi.query<Setting>(this.configPrefix + query)
      .pipe(
        map(setting => setting.data),
        toArray()
      );
  }

  /**
   * Save a setting.
   *
   * @param s The setting to save. Note that the new value should already be set to {@property Value}.
   */
  save(s: Setting): Observable<void>;

  /**
   * Save a setting.
   *
   * @param key The key of the configuration setting
   * @param value The new value of the setting.
   */
  save(key: string, value: any): Observable<void>;

  // save is overloaded, see above.
  save(s: Setting | string, v?: any): Observable<void> {
    if (typeof s === 'string') {
      return this.portapi.update(this.configPrefix + s, {
        Key: s,
        Value: v,
      });
    }
    return this.portapi.update(this.configPrefix + s.Key, s);
  }

  /**
   * Watch a configuration setting.
   *
   * @param key The key of the setting to watch.
   */
  watch<T extends Setting>(key: string): Observable<SettingValueType<T>> {
    return this.portapi.qsub<BaseSetting<SettingValueType<T>, any>>(this.configPrefix + key)
      .pipe(
        filter(value => value.key === this.configPrefix + key), // qsub does a query so filter for our key.
        map(value => value.data),
        map(value => value.Value !== undefined ? value.Value : value.DefaultValue),
        distinctUntilChanged(),
      )
  }

  /**
   * Tests if a value is valid for a given option.
   *
   * @param spec The option specification (as returned by get()).
   * @param value The value that should be tested.
   */
  validate<S extends Setting>(spec: S, value: SettingValueType<S>) {
    if (!spec.ValidationRegex) {
      return;
    }

    const re = new RegExp(spec.ValidationRegex);

    switch (spec.OptType) {
      case OptionType.Int:
      case OptionType.Bool:
        // todo(ppacher): do we validate that?
        return
      case OptionType.String:
        if (!re.test(value as string)) {
          throw new Error(`${value} does not match ${spec.ValidationRegex}`)
        }
        return;
      case OptionType.StringArray:
        (value as string[]).forEach(v => {
          if (!re.test(v as string)) {
            throw new Error(`${value} does not match ${spec.ValidationRegex}`)
          }
        });
        return
    }
  }
}
