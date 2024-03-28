import {
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  ElementRef,
  ViewChild,
  inject,
} from '@angular/core';
import { ImportResult, PortapiService, ProfileImportResult } from '@safing/portmaster-api';
import { SFNG_DIALOG_REF, SfngDialogRef } from '@safing/ui';
import { ActionIndicatorService } from '../../action-indicator';
import { getSelectionOffset, setSelectionOffset } from './selection';
import { Observable } from 'rxjs';

export interface ImportConfig {
  key: string;
  type: 'setting' | 'profile';
}

@Component({
  templateUrl: './import-dialog.component.html',
  styles: [
    `
      :host {
        @apply flex flex-col gap-2 overflow-hidden;
        min-height: 24rem;
        min-width: 24rem;
        max-height: 40rem;
        max-width: 40rem;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ImportDialogComponent {
  readonly dialogRef: SfngDialogRef<
    ImportDialogComponent,
    unknown,
    ImportConfig
  > = inject(SFNG_DIALOG_REF);

  private readonly portapi = inject(PortapiService);
  private readonly uai = inject(ActionIndicatorService);
  private readonly cdr = inject(ChangeDetectorRef);

  @ViewChild('codeBlock', { static: true, read: ElementRef })
  codeBlockElement!: ElementRef<HTMLElement>;

  result: ImportResult | ProfileImportResult | null = null;
  reset = false;
  allowUnknown = false;
  triggerRestart = false;
  allowReplace = false;

  get replacedProfiles() {
    if (this.result === null) {
      return []
    }

    if ('replacesProfiles' in this.result) {
      return this.result.replacesProfiles || [];
    }

    return [];
  }

  errorMessage: string = '';

  get scope() {
    return this.dialogRef.data;
  }

  onBlur() {
    const text = this.codeBlockElement.nativeElement.innerText;
    this.updateAndValidate(text);
  }

  onPaste(event: ClipboardEvent) {
    event.stopPropagation();
    event.preventDefault();

    // Get pasted data via clipboard API
    const clipboardData = event.clipboardData || (window as any).clipboardData;
    const text = clipboardData.getData('Text');

    this.updateAndValidate(text);
  }

  import() {
    const text = this.codeBlockElement.nativeElement.innerText;

    let saveFunc: Observable<ImportResult>;

    if (this.dialogRef.data.type === 'setting') {
      saveFunc = this.portapi.importSettings(
        text,
        this.dialogRef.data.key,
        'text/yaml',
        this.reset,
        this.allowUnknown
      );
    } else {
      saveFunc = this.portapi.importProfile(
        text,
        'text/yaml',
        this.reset,
        this.allowUnknown,
        this.allowReplace
      );
    }

    saveFunc.subscribe({
      next: (result) => {
        let msg = '';
        if (result.restartRequired) {
          if (this.triggerRestart) {
            this.portapi.restartPortmaster().subscribe();
            msg = 'Portmaster will be restarted now.';
          } else {
            msg = 'Please restart Portmaster to apply the new settings.';
          }
        }

        this.uai.success('Settings Imported Successfully', msg);
        this.dialogRef.close();
      },
      error: (err) => {
        this.uai.error(
          'Failed To Import Settings',
          this.uai.getErrorMessgae(err)
        );
      },
    });
  }

  updateAndValidate(content: string) {
    const [start, end] = getSelectionOffset(
      this.codeBlockElement.nativeElement
    );

    const p = (window as any).Prism;
    const blob = p.highlight(content, p.languages.yaml, 'yaml');
    this.codeBlockElement.nativeElement.innerHTML = blob;

    setSelectionOffset(this.codeBlockElement.nativeElement, start, end);

    if (content === '') {
      return;
    }

    window.getSelection()?.removeAllRanges();

    let validateFunc: Observable<ImportResult>;

    if (this.dialogRef.data.type === 'setting') {
      validateFunc = this.portapi.validateSettingsImport(
        content,
        this.dialogRef.data.key,
        'text/yaml'
      );
    } else {
      validateFunc = this.portapi.validateProfileImport(content, 'text/yaml');
    }

    validateFunc.subscribe({
      next: (result) => {
        this.result = result;
        this.errorMessage = '';

        this.cdr.markForCheck();
      },
      error: (err) => {
        const msg = this.uai.getErrorMessgae(err);
        this.errorMessage = msg;
        this.result = null;

        this.cdr.markForCheck();
      },
    });
  }

  loadFile(event: Event) {
    const file: File = (event.target as any).files[0];
    if (!file) {
      this.updateAndValidate('');

      return;
    }

    const reader = new FileReader();

    reader.onload = (data) => {
      (event.target as any).value = '';

      let content = (data.target as any).result;
      this.updateAndValidate(content);
    };

    reader.readAsText(file);
  }
}
