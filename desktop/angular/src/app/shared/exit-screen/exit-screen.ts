import { OverlayRef } from '@angular/cdk/overlay';
import { Component, Inject, InjectionToken } from '@angular/core';
import { SfngDialogRef, SFNG_DIALOG_REF } from '@safing/ui';
import { Observable, of } from 'rxjs';
import { map, switchMap } from 'rxjs/operators';
import { UIStateService } from 'src/app/services';
import { fadeInAnimation, fadeOutAnimation } from '../animations';

export const OVERLAYREF = new InjectionToken<OverlayRef>('OverlayRef');

@Component({
  templateUrl: './exit-screen.html',
  styleUrls: ['./exit-screen.scss'],
  animations: [
    fadeInAnimation,
    fadeOutAnimation,
  ]
})
export class ExitScreenComponent {
  constructor(
    @Inject(SFNG_DIALOG_REF) private _dialogRef: SfngDialogRef<any>,
    private stateService: UIStateService,
  ) { }

  /** @private - used as ngModel form the template */
  neveragain: boolean = false;

  closeUI() {
    const closeObserver = {
      next: () => {
        this._dialogRef.close('exit');
      }
    }

    let close: Observable<any> = of(null);
    if (this.neveragain) {
      close = this.stateService.uiState()
        .pipe(
          map(state => {
            state.hideExitScreen = true;
            return state;
          }),
          switchMap(state => this.stateService.saveState(state)),
        )
    }
    close.subscribe(closeObserver)
  }

  cancel() {
    this._dialogRef.close()
  }
}
