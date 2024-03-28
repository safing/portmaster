import { CdkDragDrop, moveItemInArray } from '@angular/cdk/drag-drop';
import {
  ChangeDetectorRef,
  Component,
  Inject,
  OnDestroy,
  OnInit,
  TrackByFunction,
} from '@angular/core';
import {
  AppProfile,
  AppProfileService,
  FingerpringOperation,
  Fingerprint,
  FingerprintType,
  PORTMASTER_HTTP_API_ENDPOINT,
  PortapiService,
  Record,
  TagDescription,
  mergeDeep,
} from '@safing/portmaster-api';
import { SFNG_DIALOG_REF, SfngDialogRef, SfngDialogService } from '@safing/ui';
import { Observable, Subject, map, of, switchMap, takeUntil } from 'rxjs';
import { ActionIndicatorService } from 'src/app/shared/action-indicator';

@Component({
  templateUrl: './edit-profile-dialog.html',
  //changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrls: ['./edit-profile-dialog.scss'],
})
// eslint-disable-next-line @angular-eslint/component-class-suffix
export class EditProfileDialog implements OnInit, OnDestroy {
  private destory$ = new Subject<void>();

  profile: Partial<AppProfile> = {
    ID: '',
    Source: 'local',
    Name: '',
    Description: '',
    Icons: [],
    Fingerprints: [],
  };

  isEditMode = false;
  iconData: string | ArrayBuffer = '';
  iconType: string = '';
  iconChanged = false;
  iconObjectURL = '';
  imageError: string | null = null;

  allProfiles: AppProfile[] = [];

  copySettingsFrom: AppProfile[] = [];

  selectedCopyFrom: AppProfile | null = null;

  fingerPrintTypes = FingerprintType;
  fingerPrintOperations = FingerpringOperation;
  processTags: TagDescription[] = [];

  trackFingerPrint: TrackByFunction<Fingerprint> = (
    _: number,
    fp: Fingerprint
  ) => `${fp.Type}-${fp.Key}-${fp.Operation}-${fp.Value}`;

  constructor(
    @Inject(SFNG_DIALOG_REF)
    private dialgoRef: SfngDialogRef<
      EditProfileDialog,
      any,
      string | null | AppProfile
    >,
    private profileService: AppProfileService,
    private portapi: PortapiService,
    private actionIndicator: ActionIndicatorService,
    private dialog: SfngDialogService,
    private cdr: ChangeDetectorRef,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) private httpAPI: string
  ) { }

  ngOnInit(): void {
    this.profileService.tagDescriptions().subscribe((result) => {
      this.processTags = result;
      this.cdr.markForCheck();
    });

    this.profileService
      .watchProfiles()
      .pipe(takeUntil(this.destory$))
      .subscribe((profiles) => {
        this.allProfiles = profiles;
        this.cdr.markForCheck();
      });

    if (!!this.dialgoRef.data && typeof this.dialgoRef.data === 'string') {
      this.isEditMode = true;
      this.profileService
        .getAppProfile(this.dialgoRef.data)
        .subscribe((profile) => {
          this.profile = profile;
          this.loadIcon();
        });
    } else if (
      !!this.dialgoRef.data &&
      typeof this.dialgoRef.data === 'object'
    ) {
      this.profile = this.dialgoRef.data;
      this.loadIcon();
    }
  }

  private loadIcon() {
    if (!this.profile.Icons?.length) {
      return;
    }

    const firstIcon = this.profile.Icons[0];

    // get the current icon of the profile
    switch (firstIcon.Type) {
      case 'database':
        this.portapi
          .get<Record & { iconData: string }>(firstIcon.Value)
          .subscribe((data) => {
            this.iconData = data.iconData;
            this.iconObjectURL = this.iconData;
            this.cdr.markForCheck();
          });
        break;

      case 'api':
        this.iconData = `${this.httpAPI}/v1/profile/icon/${firstIcon.Value}`;
        this.iconObjectURL = this.iconData;

        break;

      default:
        console.error(`Unsupported icon type ${firstIcon.Type}`);
    }

    this.cdr.markForCheck();
  }

  ngOnDestroy() {
    this.destory$.next();
    this.destory$.complete();
  }

  addFingerprint() {
    this.profile.Fingerprints?.push({
      Key: '',
      Operation: FingerpringOperation.Equal,
      Value: '',
      Type: FingerprintType.Path,
    });
  }

  removeFingerprint(idx: number) {
    this.profile.Fingerprints?.splice(idx, 1);
    this.profile.Fingerprints = [...this.profile.Fingerprints!];
  }

  removeCopyFrom(idx: number) {
    this.copySettingsFrom.splice(idx, 1);
    this.copySettingsFrom = [...this.copySettingsFrom];
  }

  addCopyFrom() {
    this.copySettingsFrom = [...this.copySettingsFrom, this.selectedCopyFrom!];
    this.selectedCopyFrom = null;
  }

  drop(event: CdkDragDrop<string[]>) {
    // create a copy of the array
    this.copySettingsFrom = [...this.copySettingsFrom];
    moveItemInArray(
      this.copySettingsFrom,
      event.previousIndex,
      event.currentIndex
    );

    this.cdr.markForCheck();
  }

  deleteProfile() {
    this.dialog
      .confirm({
        caption: 'Caution',
        header: 'Confirm Profile Deletion',
        message: 'Do you want to delete this profile?',
        buttons: [
          {
            id: 'delete',
            class: 'danger',
            text: 'Delete',
          },
          {
            id: 'abort',
            class: 'outline',
            text: 'Cancel',
          },
        ],
      })
      .onAction('delete', () => {
        this.profileService
          .deleteProfile(this.profile as AppProfile)
          .subscribe({
            next: () => this.dialgoRef.close('deleted'),
            error: (err) => {
              this.actionIndicator.error('Failed to delete profile', err);
            },
          });
      });
  }

  resetIcon() {
    this.iconChanged = true;
    this.iconData = '';
    this.iconType = '';
    this.iconObjectURL = '';
  }

  save() {
    if (!this.profile.ID) {
      this.profile.ID = this.uuidv4();
    }

    if (!this.profile.Source) {
      this.profile.Source = 'local';
    }

    let updateIcon: Observable<any> = of(undefined);

    if (this.iconChanged) {
      // delete any previously set icon
      this.profile.Icons?.forEach((icon) => {
        if (icon.Type === 'database') {
          this.portapi.delete(icon.Value).subscribe();
        }

        // FIXME(ppacher): we cannot yet delete API based icons ...
      });

      if (this.iconData !== '') {
        // save the new icon in the cache database

        // FIXME(ppacher): we currently need to calls because the icon API in portmaster
        // does not update the profile but just saves the file and returns the filename.
        // So we still need to update the profile manually.
        updateIcon = this.profileService
          .setProfileIcon(this.iconData, this.iconType)
          .pipe(
            map(({ filename }) => {
              this.profile.Icons = [
                {
                  Type: 'api',
                  Value: filename,
                  Source: 'user',
                },
              ];
            })
          );

        // FIXME(ppacher): reset presentationpath
      } else {
        // just clear out that there was an icon
        this.profile.Icons = [];
      }
    }

    if (this.profile.Fingerprints!.length > 1) {
      this.profile.PresentationPath = '';
    }
    const oldConfig = this.profile.Config || {};
    this.profile.Config = {};

    mergeDeep(
      this.profile.Config,
      ...[...this.copySettingsFrom.map((p) => p.Config || {}), oldConfig]
    );

    updateIcon
      .pipe(
        switchMap(() => {
          return this.profileService.saveProfile(this.profile as AppProfile);
        })
      )
      .subscribe({
        next: () => {
          this.actionIndicator.success(
            this.profile.Name!,
            'Profile saved successfully'
          );
          this.dialgoRef.close('saved');
        },
        error: (err) => {
          this.actionIndicator.error('Failed to save profile', err);
        },
      });
  }

  abort() {
    this.dialgoRef.close('abort');
  }

  fileChangeEvent(fileInput: any) {
    this.imageError = null;
    this.iconData = '';
    this.iconChanged = true;

    if (fileInput.target.files && fileInput.target.files[0]) {
      const max_size = 10 * 1024;
      const allowed_types = [
        'image/png',
        'image/jpeg',
        'image/svg',
        'image/gif',
        'image/tiff',
      ];
      const max_height = 512;
      const max_width = 512;
      const file: File = fileInput.target.files[0];

      if (file.size > max_size) {
        this.imageError = 'Maximum size allowed is ' + max_size / 1000 + 'KB';
      }

      if (!allowed_types.includes(file.type)) {
        this.imageError = 'Only JPG, PNG, SVG, GIF or Tiff files are allowed';
      }

      this.iconType = file.type;

      const reader = new FileReader();
      reader.onload = (e: ProgressEvent<FileReader>) => {
        const content: ArrayBuffer = e.target!.result! as ArrayBuffer;
        const blob = new Blob([content], { type: file.type });

        const image = new Image();
        image.src = URL.createObjectURL(blob);
        this.iconObjectURL = image.src;

        image.onload = (rs: any) => {
          const img_height = rs.currentTarget['height']!;
          const img_width = rs.currentTarget['width'];

          if (img_height > max_height && img_width > max_width) {
            this.imageError =
              'Maximum dimentions allowed ' +
              max_height +
              '*' +
              max_width +
              'px';
          } else {
            this.iconData = content;
          }

          this.cdr.markForCheck();
        };

        image.onerror = (err: any) => {
          this.actionIndicator.error(
            'Failed to get image',
            this.actionIndicator.getErrorMessgae(err)
          );
        };

        this.cdr.markForCheck();
      };

      reader.onerror = (err: any) => {
        this.actionIndicator.error(
          'Failed to get image',
          this.actionIndicator.getErrorMessgae(err)
        );
      };

      reader.readAsArrayBuffer(fileInput.target.files[0]);
    }
  }

  private uuidv4(): string {
    if (typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID();
    }

    // This one is not really random and not RFC compliant but serves enough for fallback
    // purposes if the UI is opened in a browser that does not yet support randomUUID
    console.warn('Using browser with lacking support for crypto.randomUUID()');

    return Date.now().toString(36) + Math.random().toString(36).substring(2);
  }
}
