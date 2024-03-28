import { KeyValue } from '@angular/common';
import { ChangeDetectionStrategy, Component, Inject } from '@angular/core';
import { AppProfile, AppProfileService, FingerpringOperation, Fingerprint, FingerprintType, PortapiService, Process } from '@safing/portmaster-api';
import { SfngDialogRef, SfngDialogService, SFNG_DIALOG_REF } from '@safing/ui';
import { EditProfileDialog } from '../edit-profile-dialog';

@Component({
  selector: 'app-process-details',
  templateUrl: './process-details-dialog.html',
  styleUrls: ['./process-details-dialog.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProcessDetailsDialogComponent {
  process: (Process & { ID: string });

  constructor(
    @Inject(SFNG_DIALOG_REF) private dialogRef: SfngDialogRef<any, any, Process>,
    private dialog: SfngDialogService,
    private portapi: PortapiService,
    private profileService: AppProfileService
  ) {
    this.process = {
      ...this.dialogRef.data,
      ID: this.dialogRef.data.PrimaryProfileID,
    }
  }

  close() {
    this.dialogRef.close();
  }

  createProfileForPath() {
    this.createProfileWithFingerprint({
      Type: FingerprintType.Path,
      Key: '',
      Value: this.process.MatchingPath || this.process.Path,
      Operation: FingerpringOperation.Equal,
    })
  }

  createProfileForCmdline() {
    this.createProfileWithFingerprint({
      Type: FingerprintType.Cmdline,
      Key: '',
      Value: this.process.CmdLine,
      Operation: FingerpringOperation.Equal,
    })
  }

  createProfileForEnv(env: KeyValue<string, string>) {
    const fp: Fingerprint = {
      Type: FingerprintType.Env,
      Key: env.key,
      Value: env.value,
      Operation: FingerpringOperation.Equal,
    }

    this.createProfileWithFingerprint(fp)
  }

  openParent() {
    if (!!this.process.ParentPid) {
      this.portapi.get<Process>(`network:tree/${this.process.ParentPid}-${this.process.ParentCreatedAt}`)
        .subscribe(process => {
          this.process = {
            ...process,
            ID: process.PrimaryProfileID,
          };
        })
    }
  }

  openGroup() {
    this.profileService.getProcessByPid(this.process.Pid)
      .subscribe(result => {
        if (!result) {
          return;
        }

        this.process = {
          ...result,
          ID: result.PrimaryProfileID
        };
      })
  }

  private createProfileWithFingerprint(fp: Fingerprint) {
    let profilePreset: Partial<AppProfile> = {
      Fingerprints: [
        fp
      ]
    };

    this.dialog.create(EditProfileDialog, {
      data: profilePreset,
      backdrop: true,
      autoclose: false,
    })

    this.dialogRef.close();
  }
}
