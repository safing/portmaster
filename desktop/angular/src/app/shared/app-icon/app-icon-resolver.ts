import { Injectable, inject, isDevMode } from "@angular/core";
import { AppProfile, AppProfileService, Icon, Process, deepClone } from "@safing/portmaster-api";
import { Observable, Subject, firstValueFrom } from "rxjs";
import { INTEGRATION_SERVICE, ProcessInfo } from "src/app/integration";
import * as parseDataURL from 'data-urls';

export abstract class AppIconResolver {
  abstract resolveIcon(profile: AppProfile): void;

  /**
   * Reports whether the last resolution for the profile (key `source/ID`)
   * was conclusive and found nothing: every candidate was looked up and the
   * integration answered "not found" for each. Lookups the integration does
   * not support, transport errors and failures to store a found icon are
   * inconclusive and never reported here.
   */
  abstract hasNoIcon(key: string): boolean;

  /**
   * The icon of the application a helper profile ships inside of, when the
   * last resolution identified one. This is a display suggestion for the
   * current UI session only; it is never stored on the profile.
   */
  abstract inheritedIcon(key: string): Icon | null;

  /** Emits the profile key whenever a resolution ends, found or not. */
  abstract readonly outcome$: Observable<string>;
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

function isLauncher(path: string | undefined): boolean {
  return !!path && launcherPaths.includes(path);
}

function sameLookup(a: ProcessInfo, b: ProcessInfo): boolean {
  return a.execPath === b.execPath && a.cmdline === b.cmdline && a.matchingPath === b.matchingPath;
}

/**
 * Directories that hold binaries of many unrelated applications. A donor
 * binary that lives directly in one of these says nothing about the
 * binaries below it.
 */
const genericDirectories: RegExp[] = [
  /^\/$/,
  /^\/(bin|sbin|lib|lib64|libexec|opt|app|tmp|var|snap|etc|run|home|usr|srv|mnt|media)$/,
  /^\/usr(\/local)?\/(bin|sbin|lib|lib64|libexec|share|games)$/,
  /^\/usr(\/local)?\/lib\/x86_64-linux-gnu$/,
  /^\/usr\/lib\/(systemd|git-core|apt|apt\/methods)$/,
  /^\/app\/(bin|lib|libexec|extra|share|opt)$/,
  /^\/home\/[^/]+$/,
  /^\/home\/[^/]+\/(bin|Apps|Applications|AppImages|Downloads|\.local|\.local\/bin|\.local\/share|\.var|\.var\/app|\.cargo|\.cargo\/bin|\.rustup|\.rustup\/toolchains)$/,
  /^\/home\/[^/]+\/\.local\/share\/(flatpak|applications)$/,
];

function isGenericDirectory(dir: string): boolean {
  return genericDirectories.some(re => re.test(dir));
}

function dirname(path: string): string {
  const i = path.lastIndexOf('/');
  return i <= 0 ? '/' : path.slice(0, i);
}

/** True for an absolute path without empty, "." or ".." components. */
function isCanonicalPath(path: string): boolean {
  if (!path.startsWith('/')) {
    return false;
  }
  return path.slice(1).split('/').every(c => c !== '' && c !== '.' && c !== '..');
}

/**
 * Heuristic for the layout of bundled helpers: donor's directory is a proper
 * ancestor of helper's directory (not the same directory, not a sibling
 * tree) and is not a generic binary directory. /usr/lib/chatgpt/ChatGPT is
 * above /usr/lib/chatgpt/resources/codex; /usr/bin/a is not above
 * /usr/share/b/bin/b, and /opt/v/a/bin/a is not above /opt/v/b/bin/b.
 * It is not proof of a relationship: /opt/acme/updater is "above"
 * /opt/acme/editor/bin/editor, and an AppImage in a folder is "above"
 * anything installed in a subfolder of it. That is why the result is only
 * ever displayed, never stored. Paths must be canonical; symlinks are not
 * resolved.
 */
function isApplicationAbove(donor: string, helper: string): boolean {
  if (!isCanonicalPath(donor) || !isCanonicalPath(helper)) {
    return false;
  }
  const donorDir = dirname(donor);
  const helperDir = dirname(helper);
  return helperDir !== donorDir
    && helperDir.startsWith(donorDir + '/')
    && !isGenericDirectory(donorDir);
}

function profilePath(p: AppProfile): string {
  return p.PresentationPath || p.LinkedPath || '';
}

/**
 * How a profile is identified for inheritance: not a flatpak at all, exactly
 * one flatpak (a single `tag/flatpak-id/equals` fingerprint), or ambiguous
 * (several flatpak fingerprints, or a broader operation such as prefix or
 * regex). Ambiguous profiles neither give nor take icons.
 */
type Identity = { kind: 'native' } | { kind: 'flatpak', id: string } | { kind: 'ambiguous' };

function identityOf(p: AppProfile): Identity {
  const fps = (p.Fingerprints || []).filter(f => f.Type === 'tag' && f.Key === 'flatpak-id');
  if (fps.length === 0) {
    return { kind: 'native' };
  }
  if (fps.length === 1 && fps[0].Operation === 'equals' && !!fps[0].Value) {
    return { kind: 'flatpak', id: fps[0].Value };
  }
  return { kind: 'ambiguous' };
}

/** The fields an icon is chosen for, in a comparable form. */
function attributionInputs(p: AppProfile): string {
  return JSON.stringify([p.PresentationPath || '', p.LinkedPath || '', p.Fingerprints || []]);
}

function sameIconRef(a: Icon, b: Icon): boolean {
  return a.Type === b.Type && a.Value === b.Value;
}

/** Outcome of one candidate lookup. */
type LookupOutcome = 'found' | 'not-found' | 'unsupported' | 'failed';

/**
 * Maps an integration rejection to an outcome. Only the documented answers
 * are recognised: the browser integration's "Not supported in browser", the
 * Tauri command's "Unsupported OS" (non-Linux builds) and its "failed to find
 * app info" miss and the exact "not found" of its cached negative entry
 * (desktop/tauri/src-tauri/src/xdg/mod.rs). Anything else, including a
 * missing command or a transport error, is a failure from which nothing
 * about the binary can be concluded.
 */
function classifyLookupError(err: unknown): LookupOutcome {
  const text = typeof err === 'string' ? err : (err as any)?.error ?? (err as any)?.message ?? '';
  const msg = `${text}`;
  if (msg.startsWith('Not supported in browser') || msg.startsWith('Unsupported OS')) {
    return 'unsupported';
  }
  if (msg.includes('failed to find app info') || msg.trim() === 'not found') {
    // A fresh miss, or the cached negative entry of the same lookup.
    return 'not-found';
  }
  return 'failed';
}

@Injectable()
export class DefaultIconResolver extends AppIconResolver {
  private integration = inject(INTEGRATION_SERVICE);
  private profileService = inject(AppProfileService);

  private pendingResolvers = new Map<string, Promise<void>>();

  /** Keys of profiles whose last resolution was conclusive and found nothing. */
  private noIcon = new Set<string>();

  /** Display-only icons of the application a helper ships inside of. */
  private inherited = new Map<string, Icon>();

  /** One profile snapshot shared by concurrent inheritance lookups. */
  private profilesSnapshot: { at: number, promise: Promise<AppProfile[]> } | null = null;

  private outcomeSubject = new Subject<string>();
  readonly outcome$ = this.outcomeSubject.asObservable();

  hasNoIcon(key: string): boolean {
    return this.noIcon.has(key);
  }

  inheritedIcon(key: string): Icon | null {
    return this.inherited.get(key) ?? null;
  }

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

    // Every resolution starts from a clean slate for this profile.
    this.noIcon.delete(key);
    this.inherited.delete(key);

    const promise = this.resolveInStages(profile)
      .then(outcome => {
        if (outcome === 'not-found' && !!profilePath(profile)) {
          this.noIcon.add(key);
        }
      })
      .catch(err => {
        this.noIcon.delete(key);
        if (isDevMode()) {
          console.log(`[icon:${profile.Name}] resolution failed: ${err}`)
        }
      })
      .finally(() => {
        this.pendingResolvers.delete(key);
        this.outcomeSubject.next(key);
      });
    this.pendingResolvers.set(key, promise);
  }

  /**
   * Tries candidate lookups in order of reliability and stops at the first
   * icon that could be saved:
   *   1. the profile's own paths (what the profile is about),
   *   2. the processes that match the profile themselves,
   *   3. their process group leaders, which may carry a desktop entry the
   *      binary itself lacks,
   *   4. for display only, the icon of another profile that belongs to the
   *      same application, for helpers that have no desktop entry of their
   *      own (see inheritFromApplication).
   * Known launchers are skipped in stages 2 and 3: a core without the
   * `leaders` parameter ignores it and answers stage 2 with leaders, so the
   * filter cannot be left to stage 3 alone. Later stages are only requested
   * when the earlier ones found nothing.
   *
   * A found icon is still the integration's best desktop-entry match for the
   * candidate, which is matched loosely (by executable and name substrings);
   * this filters listed launchers from the process fallbacks, it does not
   * make that match exact.
   *
   * The result is 'found', 'not-found' (every candidate was looked up and
   * none exists, a conclusive answer), 'unsupported' (the integration cannot
   * look icons up here) or 'failed' (a lookup, process query or store
   * operation errored; nothing can be concluded).
   */
  private async resolveInStages(profile: AppProfile): Promise<LookupOutcome> {
    const tried: ProcessInfo[] = [];
    // Kept in an object: the closures below update it, which TypeScript's
    // control-flow narrowing of a plain `let` does not see.
    const state = { worst: 'not-found' as LookupOutcome, anyLookup: false };

    const note = (o: LookupOutcome) => {
      if (o === 'unsupported' || (o === 'failed' && state.worst !== 'unsupported')) {
        state.worst = o;
      }
    };
    // Read through a function: TypeScript keeps the narrowing of a property
    // across the awaited calls that update it.
    const unsupported = (): boolean => state.worst === 'unsupported';

    const tryAll = async (candidates: ProcessInfo[]): Promise<boolean> => {
      for (const info of candidates) {
        if (!info.execPath || tried.some(t => sameLookup(t, info))) {
          continue;
        }
        tried.push(info);
        state.anyLookup = true;

        let icon: string;
        try {
          icon = await this.integration.getAppIcon(info);
        } catch (err) {
          const o = classifyLookupError(err);
          note(o);
          if (isDevMode()) {
            console.log(`[icon:${profile.Name}] ${o} via ${info.execPath}: ${JSON.stringify(err)}`)
          }
          if (o === 'unsupported') {
            return false;
          }
          continue;
        }

        try {
          await this.saveIcon(icon, profile);
          return true;
        } catch (err) {
          // The icon exists; only storing it failed. Not a "no icon" answer.
          note('failed');
          if (isDevMode()) {
            console.log(`[icon:${profile.Name}] found via ${info.execPath} but could not store it: ${err}`)
          }
          return false;
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
      return 'found';
    }
    if (unsupported()) {
      // Nothing below can do better than the integration, and inheritance
      // must not store guesses from an environment that cannot look icons
      // up (the browser UI keeps its letters).
      return 'unsupported';
    }

    // Stage 2: the owning processes, with their real command lines and
    // matching paths (for example an AppImage's original location).
    const owning = await this.processesOf(profile, false);
    if (owning === null) {
      note('failed');
    } else if (await tryAll(owning.filter(p => !isLauncher(p.Path)).map(toProcessInfo))) {
      return 'found';
    }

    // Stage 3: process group leaders.
    const leaders = await this.processesOf(profile, true);
    if (leaders === null) {
      note('failed');
    } else if (await tryAll(leaders.filter(p => !isLauncher(p.Path)).map(toProcessInfo))) {
      return 'found';
    }

    if (unsupported()) {
      return 'unsupported';
    }

    // Stage 4: the application this helper ships inside of (display only).
    const inherited = await this.inheritFromApplication(profile);
    if (inherited === 'found') {
      return 'found';
    }
    if (inherited === 'failed') {
      note('failed');
    }

    return state.anyLookup ? state.worst : 'failed';
  }

  /** Returns the processes, or null when the query failed. */
  private async processesOf(profile: AppProfile, leaders: boolean): Promise<Process[] | null> {
    try {
      const processes = await firstValueFrom(this.profileService.getProcessesByProfile(profile, leaders));
      // BUG: Portmaster sometimes returns a null entry, skip it here.
      return (processes || []).filter(p => !!p);
    } catch (err) {
      if (isDevMode()) {
        console.log(`[icon:${profile.Name}] failed to load processes (leaders=${leaders}): ${err}`)
      }
      return null;
    }
  }

  private profiles(): Promise<AppProfile[]> {
    const now = Date.now();
    if (this.profilesSnapshot === null || now - this.profilesSnapshot.at > 2000) {
      this.profilesSnapshot = {
        at: now,
        promise: firstValueFrom(this.profileService.watchProfiles()).then(all => (all || []).filter(p => !!p)),
      };
    }
    return this.profilesSnapshot.promise;
  }


  /**
   * Helpers that ship inside an application (the agent backend of a chat
   * app, the overlay and web helpers of Steam) have no desktop entry of
   * their own. If another profile of the same application has an icon that
   * core or the user set, that icon is shown for this profile too, for the
   * current UI session; nothing is written to the profile, so a wrong
   * association can only ever be a display defect of this code, never
   * stored data. "Same application" means: the same exact flatpak identity,
   * or, for two native (non-flatpak) profiles, the donor binary sitting in
   * a non-generic directory above the helper's directory, which is how
   * bundled helpers are laid out (/usr/lib/<app>/<app> above
   * /usr/lib/<app>/resources/<helper>). Profiles with an ambiguous identity
   * neither give nor take. Icons the UI guessed are never donors. If the
   * eligible donors of the best rank carry different icons, nothing is
   * shown. Returns 'found', 'not-found' or 'failed'.
   */
  private async inheritFromApplication(profile: AppProfile): Promise<LookupOutcome> {
    const key = `${profile.Source}/${profile.ID}`;
    const path = profilePath(profile);
    const me = identityOf(profile);
    if (!path || me.kind === 'ambiguous') {
      return 'not-found';
    }

    let all: AppProfile[];
    try {
      all = await this.profiles();
    } catch (err) {
      if (isDevMode()) {
        console.log(`[icon:${profile.Name}] failed to load profiles: ${err}`)
      }
      return 'failed';
    }

    const trusted = (p: AppProfile) =>
      !!p.Icons?.length && (p.Icons[0].Type === 'api' || p.Icons[0].Type === 'database')
      && !!p.Icons[0].Value && (p.Icons[0].Source === 'user' || p.Icons[0].Source === 'core');
    const related = (p: AppProfile): boolean => {
      const other = identityOf(p);
      if (me.kind === 'flatpak') {
        return other.kind === 'flatpak' && other.id === me.id;
      }
      return other.kind === 'native' && isApplicationAbove(profilePath(p), path);
    };
    const candidates = all.filter(p =>
      p.Source === 'local' && p.ID !== profile.ID && !p.ID.startsWith('_') && !!profilePath(p) && trusted(p) && related(p)
    );
    if (candidates.length === 0) {
      return 'not-found';
    }

    const rank = (p: AppProfile): number => p.Icons[0].Source === 'user' ? 0 : 1;
    const best = Math.min(...candidates.map(rank));
    const donors = candidates.filter(c => rank(c) === best);
    const icon = donors[0].Icons[0];
    if (donors.some(d => !sameIconRef(d.Icons[0], icon))) {
      if (isDevMode()) {
        console.log(`[icon:${profile.Name}] donors disagree (${donors.map(d => d.Name).join(', ')}), not inheriting`)
      }
      return 'not-found';
    }

    if (isDevMode()) {
      console.log(`[icon:${profile.Name}] showing the icon of ${donors[0].Name} (${profilePath(donors[0])})`)
    }
    this.inherited.set(key, { Type: icon.Type, Value: icon.Value, Source: 'ui' });
    return 'found';
  }

  /**
   * Appends an icon reference to the profile and saves it. The profile is
   * re-read first so that an icon or edit made while the resolution was in
   * flight is not overwritten by the snapshot the resolution started from.
   * If the profile acquired an icon meanwhile, or its paths or fingerprints
   * (the inputs the icon was chosen for) changed, nothing is written.
   */
  private async storeIconReference(profile: AppProfile, icon: Icon): Promise<void> {
    const current = await firstValueFrom(this.profileService.getAppProfile(profile.Source, profile.ID));
    if (!!current.Icons?.length || attributionInputs(current) !== attributionInputs(profile)) {
      if (isDevMode()) {
        console.log(`[icon:${profile.Name}] profile changed during resolution, not storing the icon`)
      }
      return;
    }
    const updated = deepClone(current);
    updated.Icons = [icon];
    await firstValueFrom(this.profileService.saveProfile(updated));
  }

  /** Uploads an icon the integration found and stores a reference to it in the profile. */
  private async saveIcon(icon: string, profile: AppProfile): Promise<void> {
    const dataURL = parseDataURL(icon);
    if (!dataURL) {
      throw new Error("invalid data url");
    }
    const blob = new Blob([dataURL.body], {
      type: dataURL.mimeType.essence,
    })

    const body = await blob.arrayBuffer();
    const result = await firstValueFrom(this.profileService.setProfileIcon(body, blob.type));
    await this.storeIconReference(profile, { Value: result.filename, Type: 'api', Source: 'ui' });
  }
}

function toProcessInfo(process: Process): ProcessInfo {
  return {
    execPath: process.Path,
    cmdline: process.CmdLine,
    pid: process.Pid,
    matchingPath: process.MatchingPath,
  };
}
