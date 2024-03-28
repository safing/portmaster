import { Injectable } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { distinctUntilChanged, map } from 'rxjs/operators';

/**
 * SessionDataService is used to store transient data
 * that are only important as long as the application is
 * being used. Those data are not presisted and are
 * removed once the application is restarted.
 */
@Injectable({
  providedIn: 'root'
})
export class SessionDataService {
  private data = new Map<string, any>();
  private stream = new BehaviorSubject<void>(undefined);

  /** Set sets a value in the session data service */
  set<T>(key: string, value: T): void {
    this.data.set(key, value);
  }

  get<T>(key: string): T | null;
  get<T>(key: string, def: T): T;

  /** Get retrieves a value from the session data service */
  get(key: string, def?: any): any {
    const value = this.data.get(key);
    if (value !== undefined) {
      return value;
    }

    if (def !== undefined) {
      return def;
    }
    return null;
  }

  watch<T>(key: string): Observable<T | null>;
  watch<T>(key: string, def: T): Observable<T>;

  /** Watch a key for changes to it's identity. */
  watch<T>(key: string, def?: any): Observable<T | null> {
    return this.stream
      .pipe(
        map(() => this.get<T>(key, def)),
        distinctUntilChanged()
      );
  }

  delete<T>(key: string): T | null {
    let value = this.get<T>(key);
    if (value !== null) {
      this.data.delete(key);
    }
    return value;
  }

  save<M, K extends keyof M>(id: string, model: M, keys: K[]) {
    let copy: Partial<M> = {};
    keys.forEach(key => copy[key] = model[key]);
    this.set(id, copy);
  }

  restore<M extends object, K extends keyof M>(id: string, model: M) {
    let copy: Partial<M> | null = this.get(id);
    if (copy === null) {
      return;
    }
    Object.assign(model, copy);
  }
}
