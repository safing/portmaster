import { AppProfile } from './../../../../../dist-lib/safing/portmaster-api/lib/app-profile.types.d';
import { ChangeDetectionStrategy, Component, OnInit, TrackByFunction, inject } from "@angular/core";
import { Router } from '@angular/router';
import { PortapiService } from '@safing/portmaster-api';
import { SFNG_DIALOG_REF, SfngDialogRef } from "@safing/ui";
import { ActionIndicatorService } from 'src/app/shared/action-indicator';

@Component({
  templateUrl: './merge-profile-dialog.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [
    `
    :host {
      @apply flex flex-col gap-2 justify-start h-96 w-96;
    }
    `
  ]
})
export class MergeProfileDialogComponent implements OnInit {
  readonly dialogRef: SfngDialogRef<MergeProfileDialogComponent, unknown, AppProfile[]> = inject(SFNG_DIALOG_REF);
  private readonly portapi = inject(PortapiService);
  private readonly router = inject(Router);
  private readonly uai = inject(ActionIndicatorService);

  get profiles(): AppProfile[] {
    return this.dialogRef.data;
  }

  primary: AppProfile | null = null;
  newName = '';

  trackProfile: TrackByFunction<AppProfile> = (_, p) => `${p.Source}/${p.ID}`

  ngOnInit(): void {
    (() => { });
  }

  mergeProfiles() {
    if (!this.primary) {
      return
    }

    this.portapi.mergeProfiles(
      this.newName,
      `${this.primary.Source}/${this.primary.ID}`,
      this.profiles
        .filter(p => p !== this.primary)
        .map(p => `${p.Source}/${p.ID}`)
    )
      .subscribe({
        next: newID => {
          this.router.navigate(['/app/' + newID])
          this.uai.success('Profiles Merged Successfully', 'All selected profiles have been merged')

          this.dialogRef.close()
        },
        error: err => {
          this.uai.error('Failed To Merge Profiles', this.uai.getErrorMessgae(err))
        }
      })
  }
}
