import { Injectable, inject, isDevMode } from "@angular/core";
import { AppProfile, AppProfileService, deepClone } from "@safing/portmaster-api";
import { firstValueFrom, map, switchMap } from "rxjs";
import { INTEGRATION_SERVICE, ProcessInfo } from "src/app/integration";
import * as parseDataURL from 'data-urls';

export abstract class AppIconResolver {
  abstract resolveIcon(profile: AppProfile): void;
}

@Injectable()
export class DefaultIconResolver extends AppIconResolver {
  private integration = inject(INTEGRATION_SERVICE);
  private profileService = inject(AppProfileService);

  private pendingResolvers = new Map<string, Promise<void>>();

  resolveIcon(profile: AppProfile): void {
    const key = `${profile.Source}/${profile.ID}`;

    // if there's already a promise in flight, abort.
    if (this.pendingResolvers.has(key)) {
      if (isDevMode()) {
        console.log(`[icon:${profile.Name}] loading icon already in progress ...`)
      }

      return;
    }

    let promise = new Promise<void>((resolve) => {
      this.profileService
        .getProcessesByProfile(profile)
        .pipe(
          map(processes => {
            // if we there are no running processes for this profile,
            // we try to find the icon based on the information stored in
            // the profile.
            let info: ProcessInfo[] = [{
              execPath: profile.LinkedPath,
              cmdline: profile.PresentationPath,
              pid: -1,
              matchingPath: profile.PresentationPath,
            }]

            processes?.forEach(process => {
              // BUG: Portmaster sometimes runs a null entry, skip it here.
              if (!process) {
                return;
              }

              // insert at the beginning since the process data might reveal
              // better results than the profile one.
              info.splice(0, 0, {
                execPath: process.Path,
                cmdline: process.CmdLine,
                pid: process.Pid,
                matchingPath: process.MatchingPath,
              })
            })

            return info;
          })
        ).subscribe(async (processInfos) => {
          for (const info of processInfos) {
            try {
              await this.loadAndSaveIcon(info, profile);

              // success, abort now
              resolve();
              return;
            } catch (err) {
              // continue using the next one
            }
          }

          // we failed to find an icon, still resolve the promise here
          // because nobody actually cares ....
          resolve();
        })
    });
    this.pendingResolvers.set(key, promise);

    promise.finally(() => this.pendingResolvers.delete(key));
  }

  private async loadAndSaveIcon(info: ProcessInfo, profile: AppProfile): Promise<void> {
    const icon = await this.integration.getAppIcon(info);

    const dataURL = parseDataURL(icon);
    if (!dataURL) {
      throw new Error("invalid data url");
    }
    const blob = new Blob([dataURL.body], {
      type: dataURL.mimeType.essence,
    })

    const body = await blob.arrayBuffer();

    const save$ = this.profileService
      .setProfileIcon(body, blob.type)
      .pipe(switchMap(result => {
        // save the profile icon
        profile = deepClone(profile);
        profile.Icons = [
          ...(profile.Icons || []),
          {
            Value: result.filename,
            Type: 'api',
            Source: 'ui'
          }
        ];

        return this.profileService.saveProfile(profile)
      }));

    await firstValueFrom(save$);
  }
}
