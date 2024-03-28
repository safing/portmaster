import { ChangeDetectionStrategy, Component, Inject, InjectionToken } from '@angular/core';
import { SfngDialogRef, SFNG_DIALOG_REF } from './dialog.ref';

export interface ConfirmDialogButton {
  text: string;
  id: string;
  class?: 'danger' | 'outline';
}

export interface ConfirmDialogConfig {
  buttons?: ConfirmDialogButton[];
  canCancel?: boolean;
  header?: string;
  message?: string;
  caption?: string;
  inputType?: 'text' | 'password';
  inputModel?: string;
  inputPlaceholder?: string;
}

export const CONFIRM_DIALOG_CONFIG = new InjectionToken<ConfirmDialogConfig>('ConfirmDialogConfig');

@Component({
  templateUrl: './confirm.dialog.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SfngConfirmDialogComponent {
  constructor(
    @Inject(SFNG_DIALOG_REF) private dialogRef: SfngDialogRef<any>,
    @Inject(CONFIRM_DIALOG_CONFIG) public config: ConfirmDialogConfig,
  ) {
    if (config.inputType !== undefined && config.inputModel === undefined) {
      config.inputModel = '';
    }
  }

  select(action?: string) {
    this.dialogRef.close(action || null);
  }
}
