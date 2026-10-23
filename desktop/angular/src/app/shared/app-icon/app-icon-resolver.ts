import { Injectable, inject, isDevMode } from "@angular/core";
import { AppProfile, AppProfileService, Process, deepClone } from "@safing/portmaster-api";
import { firstValueFrom, switchMap } from "rxjs";
import { INTEGRATION_SERVICE, ProcessInfo } from "src/app/integration";
import * as parseDataURL from 'data-urls';

export abstract class AppIconResolver {
  abstract resolveIcon(profile: AppProfile): void;
}

/**
 * Known shared launchers, excluded from the process-derived fallback
 * candidates (stages 2 and 3 below). A process with one of these paths (for
 * example the bwrap launcher every flatpak shares, or the Steam client that
 * usually leads the process group of a game it starts) tells nothing about
 * the application a profile stands for. This is a mitigation with a known,
 * finite list, not a complete classification: the profile's own path and
 * the owning processes are tried first so that leaders rarely decide the
 * icon, and a profile whose own path is a launcher is still looked up by it.
 */
const launcherPaths = [
  '/usr/bin/bwrap',
  '/usr/bin/flatpak',
  '/usr/bin/env',
  '/usr/bin/sh', '/bin/sh',
  '/usr/bin/bash', '/bin/bash',
  '/usr/bin/dash', '/bin/dash',
  '/usr/bin/zsh', '/bin/zsh',
  '/usr/bin/steam', '/usr/games/steam', '/usr/lib/steam/bin_steam.sh',
  '/usr/bin/wine', '/usr/bin/wine64', '/usr/bin/wine-preloader', '/usr/bin/wine64-preloader',
];

/**
 * True when the path is one of the known shared launchers.
 */
function isLauncher(path: string | undefined): boolean {
  return !!path && launcherPaths.includes(path);
}

/**
 * True when two candidates would be the same integration lookup (same executable, command line and matching path).
 */
function sameLookup(a: ProcessInfo, b: ProcessInfo): boolean {
  return a.execPath === b.execPath && a.cmdline === b.cmdline && a.matchingPath === b.matchingPath;
}

@Injectable()
export class DefaultIconResolver extends AppIconResolver {
  private integration = inject(INTEGRATION_SERVICE);
  private profileService = inject(AppProfileService);

  private pendingResolvers = new Map<string, Promise<void>>();

  /**
   * Starts a resolution for a non-special profile unless one is already in flight.
   */
  resolveIcon(profile: AppProfile): void {
    // Special profiles (IDs starting with "_": unidentified, unsolicited,
    // system, system resolver, Portmaster's own processes) are deliberately
    // excluded: an icon found via one of their processes would describe that
    // process, not the group of connections the profile stands for.
    if (profile.ID?.startsWith('_')) {
      return;
    }

    const key = `${profile.Source}/${profile.ID}`;

    // if there's already a promise in flight, abort.
    if (this.pendingResolvers.has(key)) {
      if (isDevMode()) {
        console.log(`[icon:${profile.Name}] loading icon already in progress ...`)
      }

      return;
    }

    const promise = this.resolveInStages(profile);
    this.pendingResolvers.set(key, promise);
    promise.finally(() => this.pendingResolvers.delete(key));
  }

  /**
   * Tries candidate lookups in order of reliability and stops at the first
   * icon that could be saved:
   *   1. the profile's own paths (what the profile is about),
   *   2. the processes that match the profile themselves,
   *   3. their process group leaders, which may carry a desktop entry the
   *      binary itself lacks.
   * Known launchers are skipped in stages 2 and 3: a core without the
   * `leaders` parameter ignores it and answers stage 2 with leaders, so the
   * filter cannot be left to stage 3 alone. Later stages are only requested
   * when the earlier ones found nothing.
   *
   * A found icon is still the integration's best desktop-entry match for the
   * candidate, which is matched loosely (by executable and name substrings);
   * this filters listed launchers from the process fallbacks, it does not
   * make that match exact.
   */
  private async resolveInStages(profile: AppProfile): Promise<void> {
    const tried: ProcessInfo[] = [];

    const tryAll = async (candidates: ProcessInfo[]): Promise<boolean> => {
      for (const info of candidates) {
        if (!info.execPath || tried.some(t => sameLookup(t, info))) {
          continue;
        }
        tried.push(info);
        try {
          await this.loadAndSaveIcon(info, profile);
          return true;
        } catch (err) {
          if (isDevMode()) {
            console.log(`[icon:${profile.Name}] no icon via ${info.execPath}: ${err}`)
          }
        }
      }
      return false;
    };

    // Stage 1: the profile's own paths.
    const own: ProcessInfo[] = [];
    for (const path of [profile.PresentationPath, profile.LinkedPath]) {
      if (!!path && !own.some(o => o.execPath === path)) {
        own.push({ execPath: path, cmdline: path, pid: -1, matchingPath: path });
      }
    }
    if (await tryAll(own)) {
      return;
    }

    // Stage 2: the owning processes, with their real command lines and
    // matching paths (for example an AppImage's original location).
    const owning = await this.processesOf(profile, false);
    if (await tryAll(owning.filter(p => !isLauncher(p.Path)).map(toProcessInfo))) {
      return;
    }

    // Stage 3: process group leaders.
    const leaders = await this.processesOf(profile, true);
    await tryAll(leaders.filter(p => !isLauncher(p.Path)).map(toProcessInfo));
  }

  /**
   * The processes of the profile, or its process group leaders; an empty list when the request fails.
   */
  private async processesOf(profile: AppProfile, leaders: boolean): Promise<Process[]> {
    try {
      const processes = await firstValueFrom(this.profileService.getProcessesByProfile(profile, leaders));
      // BUG: Portmaster sometimes returns a null entry, skip it here.
      return (processes || []).filter(p => !!p);
    } catch (err) {
      if (isDevMode()) {
        console.log(`[icon:${profile.Name}] failed to load processes (leaders=${leaders}): ${err}`)
      }
      return [];
    }
  }

  /**
   * Looks the candidate up through the integration, uploads the icon it returns and stores a
   * reference to it on the profile.
   */
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

/**
 * The integration lookup inputs of a process: executable, command line, pid and matching path.
 */
function toProcessInfo(process: Process): ProcessInfo {
  return {
    execPath: process.Path,
    cmdline: process.CmdLine,
    pid: process.Pid,
    matchingPath: process.MatchingPath,
  };
}
