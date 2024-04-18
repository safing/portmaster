import { Injectable } from "@angular/core";
import { PortapiService, Record } from '@safing/portmaster-api';
import { Observable, of } from "rxjs";
import { catchError, map, switchMap } from "rxjs/operators";
import { SortTypes } from './../shared/network-scout/network-scout';

export interface UIState extends Record {
  hideExitScreen?: boolean;
  introScreenFinished?: boolean;
  netscoutSortOrder: SortTypes;
}

const defaultState: UIState = {
  hideExitScreen: false,
  introScreenFinished: false,
  netscoutSortOrder: SortTypes.static
}

@Injectable({ providedIn: 'root' })
export class UIStateService {
  constructor(private portapi: PortapiService) { }

  uiState(): Observable<UIState> {
    const key = 'core:ui/v1';
    return this.portapi.get<UIState>(key)
      .pipe(
        catchError(err => of(defaultState)),
        map(state => {
          (Object.keys(defaultState) as (keyof UIState)[])
            .forEach(key => {
              if (state[key] === undefined) {
                (state as any)[key] = defaultState[key]!
              }
            })

          return state
        })
      )
  }

  saveState(state: UIState): Observable<void> {
    const key = 'core:ui/v1';
    return this.portapi.create(key, state);
  }

  set<K extends keyof UIState, V extends UIState[K]>(key: K, value: V): Observable<void> {
    return this.uiState()
      .pipe(
        map(state => {
          state[key] = value

          return state;
        }),
        switchMap(newState => this.saveState(newState))
      );
  }
}
