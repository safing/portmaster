import { Injectable } from '@angular/core';
import { ConfigService, ExpertiseLevel, StringSetting } from '@safing/portmaster-api';
import { BehaviorSubject, Observable } from 'rxjs';
import { distinctUntilChanged, map, repeat, share } from 'rxjs/operators';

@Injectable({
  providedIn: 'root'
})
export class ExpertiseService {
  /** If the user overwrites the expertise level on a per-page setting we track that here */
  private _localOverwrite: ExpertiseLevel | null = null;
  private _currentLevel: ExpertiseLevel = ExpertiseLevel.User;

  /** Watches the expertise level as saved in the configuration */
  private _savedLevel$ = this.configService.watch<StringSetting>('core/expertiseLevel')
    .pipe(
      repeat({ delay: 2000 }),
      map(upd => {
        return upd as ExpertiseLevel;
      }),
      distinctUntilChanged(),
      share(),
    );

  private level$ = new BehaviorSubject(ExpertiseLevel.User);

  get currentLevel() {
    return this._localOverwrite === null
      ? this._currentLevel
      : this._localOverwrite;
  }

  get savedLevel() {
    return this._currentLevel;
  }

  get change(): Observable<ExpertiseLevel> {
    return this.level$.asObservable();
  }

  constructor(private configService: ConfigService) {
    this._savedLevel$
      .subscribe(lvl => {
        this._currentLevel = lvl;
        if (this._localOverwrite === null) {
          this.level$.next(lvl);
        }
      });
  }

  setLevel(lvl: ExpertiseLevel | null) {
    if (lvl === this._currentLevel) {
      lvl = null;
    }

    this._localOverwrite = lvl;
    if (!!lvl) {
      this.level$.next(lvl);
    } else {
      this.level$.next(this._currentLevel!);
    }
  }
}
