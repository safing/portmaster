import {
  ChangeDetectionStrategy,
  ChangeDetectorRef,
  Component,
  HostBinding,
  Inject,
  Input,
  OnDestroy,
  OnInit,
  SkipSelf,
  inject,
} from '@angular/core';
import { DomSanitizer, SafeUrl } from '@angular/platform-browser';
import {
  AppProfileService,
  PORTMASTER_HTTP_API_ENDPOINT,
  PortapiService,
  Record,
  deepClone,
} from '@safing/portmaster-api';
import { Subscription, map, of } from 'rxjs';
import { switchMap } from 'rxjs/operators';
import { AppIconResolver } from './app-icon-resolver';
import { AppProfile } from 'projects/safing/portmaster-api/src/public-api';

// Interface that must be satisfied for the profile-input
// of app-icon.
export interface IDandName {
  // ID of the profile.
  ID?: string;

  // Source is the source of the profile.
  Source?: string;

  // Name of the profile.
  Name: string;
}

// Some icons we don't want to show on the UI.
// Note that this works on a best effort basis and might
// start breaking with updates to the built-in icons...
const iconBlobsToIgnore = [
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAABU0lEQVRYhe2WTUrEQBCF36i4ctm4FsdTKF5AEFxL0knuILgQXAy4ELxDfgTXguAFRG/hDXKCAbtcOB3aSVenMjPRTb5NvdCE97oq3QQYGflnJlbc3T/QXxrfXF9NAGBraKPTk2Nvtey4D1l8OUiIo8ODX/Xt/cMfQCk1SAAi8upWgLquWy8rpbB7+yk2m8+mYvNWAAB4fnlt9MX5WaP397ZhCPgygCFa1IUmwJifCgB5nrMBtdbhAK6pi9QcALIs8+5c1AEOqTmwZge4EUjNiQhpmjbarcvaG4AbgcTcUhSFfwFAHMfhABxScwBIkgRA9wnwBgiOQGBORCjLkl2PoigcgB2BwNzifmi97wEOqTkRoaoqdr2zA9wIJOYWrTW785VPQR+WO2B3vdYIpBBRc9Qkp2Cw/4GVR+BjPpt23u19tUXUgU2aBzuQPz5J8oyMjGyUb9+FOUOmulVPAAAAAElFTkSuQmCC',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAYAAACqaXHeAAACLElEQVR4nO2av07DMBDGP1DFxtaFmbeg6gtUqtQZtU3yDkgMSAxIDEi8Q/8gMVdC4m1YYO0TMNQspErdOG3Md25c7rc0st3E353v7EsLKIqiKIqiKMq/5MRueHx6NoeYSCjubm82NJ8eaiISdDtX6HauKq9tWsFmF4DPr6+d1zalBshG18RpNYfJy+tW21GFgA+lK6DdboeeBwVjyvO3qx1wGGC5XO71wCYZykc8QEqCZ/cfjNs4+X64rOz3FQ/sMMDi7R2Dfg+Lt/eN9kG/tzX24rwFA8AYYGXM+nr9aQADs9mG37FWW3HsqqBhMpnsFFRGkiTOvkoD5ELLBNtIiLcdmGXZ5jP/4Pkc2i4gIb5KRl3xrnbaQSiEeN8QGI/Hzj5aDgjh+SzLaJ7P4eWAiJZ9EVoIhBA/nU695uYdAnUI4fk0TUvbXeP3gZcDhMS7CLIL1DsHyIv3DYHRaOTs44YAZD2fpik9EfIOQohn2Rch5wBZ8bPZzOObfwiBurWAtOftoqaO511jaSEgJd4FQzwgmAQlxPuGwHA4dPbJ1QICnk+ShOb5HJlaoOHLvgi/FhAUP5/P9xpbteRtyDlA1vN2UVPH8+K7gJR45/MI4gHyK7HYxANsA7BuVvkcnniAXAtIwxYPRPTboIR4IBIDMMSL7wIhYZbF0RmgsS9EQtDY1+L5r7esCUrGvA3xHBCfeIBkgBjEi+0CMYsHHDmg7N9UiqIoiqIoiqIcFT++NKIXgDvowAAAAABJRU5ErkJggg==',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACgAAAAoCAYAAACM/rhtAAABqUlEQVRYhe2XP2rDMBSHfymhU0dDD5BbJOQCgUDmEv+7Q6FDoUOgQ6F3cJxC50Agt+nSrD5BBr8OqVyrtfWkl8ShoG+SjJE+/95DwoDH4/nf9NTg+eWVLinym8eH+x4AXF1i8/FoiPFoaBwr+p3bAfjc7dixQhNMw7szatmTvb1XY00wCILOZYjIONcEi6JoXSgIAlw/fYhF9ouBsxzQ0IPrzRaz6QTrzbZ6NptOqvHtTR8EQklAWQIl4WdOQEkEqsaHefm9b5Zl7IfEcWwWVDJ1Ke0rHeXqmaRpeljDIrlWQQ5XufreNglGUWQW5EoslQOAJEm0uagHuRJL5YgIy+Wycc06bIIcEjmFStCUnPGYASxKLJQDYJVgGIZmQZsSS+SAv0eIKblWQQ6pHBEhz3N2fTZBrsQSOYVK0JQc24N2JXaXA2CV4Hw+NwtySOUA/QixvU1kPSiQIyKsViv2vaMTlMgpoihik2N7kEMqB6AxwXpiVlfduSAi7Qix7cGL/DS5XHWdC7rIAY4l3i8GTk1+zLsKpwS7lnMS7ErOeMzU/0c9Ho/nNHwBdUH2gB9vJRsAAAAASUVORK5CYII=',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAByElEQVRYhe1WQUoDQRCsmSh4CAreo3/w4CdE8JirLzCKGhRERPBqfISQx3j0BcaDJxHNRWS7PWRmtmdmJ9mNiSuYOmyYbOiqruoeAizw36G6p0e3WulOHeTE1NO/Qb6zu1f4qZXuqLPuMV9d38xbQyEuL86ha2EWWJKHfr+P4XAIAGg2m2i32wCA7fsXPH9kABjMgHkADP87cW6tNvCwvzG2biRAvpAYvH+54mCAmUcvmI0Yq4nM74DBG02sGwlIgqigS/ZEgdkcrSAuVbpUBEyjTiP7JSkDzKZrdo+xdSMBKas4y4K8befSiVxcLnR83UhACtYBV9TOgbBbOX4TF2YZQZY5Yi9/MYwkXQjy/3EEtjp7LgQzAeOUVSo0zCACcgOnwjUEC2LE7kxApS0AGFRgP4vZ8M5VBaQjoNGKuQ20Q2ney8Gr0H0kIAU7hK4zYiPCJxtFZYRMIyAdAQWrFgyicMSfj4oCkheRmQFyIoq2IRcy9T2QhNmCfN/FVcwMBSWu4XlsQUZe5tZmZW0HBXGU4o4FpCJorS3j6fXTEOVdUrgNApvrK9UFpPB4vlWq2DSo/S+Z6p4c9rRuHNRBTsR3dfAu8LfwDdGgu25Uax8RAAAAAElFTkSuQmCC',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAAByUlEQVRYhe1WQUoDQRCs2UTwEBS8R//gwU+I4DFXX2AENRgQEcGr8RFCHuPRFxgPnkQ0F9Ht9rAzsz0zO8luTFzB1GHDZENXdVX3EGCJ/w7VO+3eJKrZrYOc+GuQ/Ab57t5+4Weiml111jvmy6vrRWsoxMV5H0ktzAJNeRgOhxiPxwCAVquFTqcDANi5e8bTewqAwQzoB8BwvxPn9loD9webE+sGAuQLidHbpy0OBpg5e8GsxRhNpH8HjF5pat1AQBREBV2yIwrM+mgEcanSpSJgyjoN7JekDDDrrtk+JtYNBMSs4jT18jadSydycbnQyXUDATEYB2xRMwfCbmX5dVyYZwRpaomd/MUwknTBy//HEZjq7LjgzQS0U0ap0DCHCMgOnPLXECyIEbozBZW2AGBQgf0sZsM5VxUQj4CyFbMbaIZSv5eDV6H7QEAMZghtZ8RahEuWRaWFzCIgHgF5q+YNonDEnY+KAqIXkZ4BsiKKtiEXMvM9EIXegnzfxVXMDAUlruFFbEFKTubGZmVsB3lxlOIOBcQiaK+v4PHlQxPlXZK/DQJbG6vVBcTw0N8uVWwW1P6XTPVOjgZJ0jisg5yIb+vgXeJv4RvrxrtwzfCUqAAAAABJRU5ErkJggg==',
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACAAAAAgCAYAAABzenr0AAADo0lEQVRYhe2Wu28cVRTGf+fcuzZeY0NCUqTgD3C8mzRU0KDQEgmqoBSGhoKWAlFEKYyQAAkrTRRqOpCQkOhAkUCio4D4IYSAzkkKB+wQbLy7c8+hmNmd2WecDQoFfNJodGfn7vc4Z84M/I9/GfLeB1cutdqH7zxSUli9fOntd4EsmrVXL1xcodVqAf6PEl37+AveWDk/dP78s08vA1eBvSgSDnd3bs49DJGKICIg+dod3J3XXn6Ogz9+49WXnu07F1gA9mOWJRqNBrNRcJ8mAQF8ZHYyuBYhI/DlV9cBAqARnBAj2agdjwARoBaETnK+/eY7NMwfaaPZPueefwaA73+4MfKeM80GAC+8+QkA19cukCQOC+ga1zDPR1//jIgjWhzBEQWNBupoNESdldNn2dm5w/FjT/SIpkEcvLAwX0PUQRwNXQGOBCvXoVpxZ31jc2ICEwWY+1y19AvzEQr3GgAtiLUUo8F690tB5DhC3sgiw800f2p/fAJ/tTtoyMOo1yOqnscdnINOIqNDO+vQbrdwMTRWEnBhfXNyAvOn9qmfOBgvwKxwC9TnAskTN3f32PnzHi1robEbv6HFUVGQJ+AOIvkQgL4U6icOqC9OSKCKu4cH/HT7Nh3P0GiEWkEcc+LBEhylB+qL+ywe+328gGrFNre3kWiE6EjsOi5EqPVS6EGEZrOJW0JVR5KMIy8TqCjQmlUcl7GLlvGrlgLcYWNzY2ICk1CUoFSgtdRPHAwtYteQeimUCuDsmebEMX7l3Pv3E1BCY+lUgqNaFZJ663ID3Fh/6ARKhFrqNVq15lVy1dRP1FjGRaZ6lQwnEKqkw+Si/QLMATwnHxhA7o65k2UJM0NwanOP30dATAPkhmjlmuYiuhCcja0fR7prNhqA4W5Fjwz3ydBTEGLZaKoV99p13y8AnGZjeeT4dfd8LrnnCYyoUQTQQsGtW7/y+tPnR7oZxPb2LywvncRd2dzaGnnP6aUlzBLJvKt1tIAsObUAF195kZ2dO0cSsLx0EgAz6yWQO3aSGeZOJ8swS5gNj+c+AeYwE4QgxlPHF6nNzkBKpGQ4EGMAnSksOGCA41nisJP/eTfuVIjAHQRCCITiPaPjBAC0kwMKMkvW7vuJTgZQffSkOBRCLqeL0cN4PKLA6trah2/FGB97wL05oSohKCEEzMBSRkpp4gf+3d3dq+SOTIAZ4Enyz+QwjYgpkIB7wF6RIxGo8eAJTgsDOpB/jP+38TcKdstukjAxWQAAAABJRU5ErkJggg==',
];
const iconIDsToIgnore = [
  "a27898ddfa4e0481b62c69faa196919a738fcade",
	"5a3eea8bcd08b9336ce9c5083f26185164268ee9",
	"573393d6ad238d255b20dc1c1b303c95debe6965",
	"d459b2cb23c27cc31ccab5025533048d5d8301bf",
	"d35a0d91ebfda81df5286f68ec5ddb1d6ad6b850",
	"cc33187385498384f1b648e23be5ef1a2e9f5f71",
];


const profilesToIgnore = ['local/_unidentified', 'local/_unsolicited'];

@Component({
  selector: 'app-icon',
  templateUrl: './app-icon.html',
  styleUrls: ['./app-icon.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AppIconComponent implements OnInit, OnDestroy {
  private sub = Subscription.EMPTY;
  private initDone = false;

  private resovler = inject(AppIconResolver);

  /** @private The data-URL for the app-icon if available */
  src: SafeUrl | string = '';

  /** The profile for which to show the app-icon */
  @Input()
  set profile(p: IDandName | null | undefined | string) {
    if (typeof p === 'string') {
      const parts = p.split("/")
      p = {
        Source: parts[0],
        ID: parts[1],
        Name: '',
      }
    }

    if (!!this._profile && !!p && this._profile.ID === p.ID) {
      // skip if this is the same profile
      return;
    }

    this._profile = p || null;

    if (this.initDone) {
      this.updateView();
    }
  }
  get profile(): IDandName | null | undefined {
    return this._profile;
  }
  private _profile: IDandName | null = null;

  /** isIgnoredProfile is set to true if the profile is part of profilesToIgnore */
  isIgnoredProfile = false;

  /** If not icon is available, this holds the first - uppercased - letter of the app - name */
  letter = '';

  /** @private The background color of the component, based on icon availability and generated by ID */
  @HostBinding('style.background-color')
  color = 'var(--text-tertiary)';

  constructor(
    private profileService: AppProfileService,
    private changeDetectorRef: ChangeDetectorRef,
    private portapi: PortapiService,
    // @HostBinding() is not evaluated in our change-detection run but rather
    // checked by the parent component during updateRenderer.
    // Since we want the background color to change immediately after we set the
    // src path we need to tell the parent (which ever it is) to update as wel.
    @SkipSelf() private parentCdr: ChangeDetectorRef,
    private sanitzier: DomSanitizer,
    @Inject(PORTMASTER_HTTP_API_ENDPOINT) private httpAPI: string
  ) { }

  /** Updates the view of the app-icon and tries to find the actual application icon */
  private requestedAnimationFrame: number | null = null;
  private updateView(skipIcon = false) {
    if (this.requestedAnimationFrame !== null) {
      cancelAnimationFrame(this.requestedAnimationFrame);
    }

    this.requestedAnimationFrame = requestAnimationFrame(() => {
      this.__updateView(skipIcon);
    })
  }

  ngOnInit(): void {
    this.updateView();
    this.initDone = true;
  }

  private __updateView(skipIcon = false) {
    this.requestedAnimationFrame = null;

    const p = this.profile;
    const sourceAndId = this.getIDAndSource();

    if (!!p && sourceAndId !== null) {
      let idx = 0;
      for (let i = 0; i < (p.ID || p.Name).length; i++) {
        idx += (p.ID || p.Name).charCodeAt(i);
      }

      const combinedID = `${sourceAndId[0]}/${sourceAndId[1]}`;
      this.isIgnoredProfile = profilesToIgnore.includes(combinedID);

      this.updateLetter(p);

      if (!this.isIgnoredProfile) {
        this.color = AppColors[idx % AppColors.length];
      } else {
        this.color = 'transparent';
      }

      if (!skipIcon) {
        this.tryGetSystemIcon();
      }

    } else {
      this.isIgnoredProfile = false;
      this.color = 'var(--text-tertiary)';
    }

    this.changeDetectorRef.markForCheck();
    this.parentCdr.markForCheck();
  }

  private updateLetter(p: IDandName) {
    if (p.Name !== '') {
      if (p.Name[0] === '<') {
        // we might get the name with search-highlighting which
        // will then include <em> tags. If the first character is a <
        // make sure to strip all HTML tags before getting [0].
        this.letter = p.Name.replace(
          /(&nbsp;|<([^>]+)>)/gi,
          ''
        )[0].toLocaleUpperCase();
      } else {
        this.letter = p.Name[0];
      }

      this.letter = this.letter.toLocaleUpperCase();
    } else {
      this.letter = '?';
    }
  }

  getIDAndSource(): [string, string] | null {
    if (!this.profile) {
      return null;
    }

    const id = this.profile.ID;
    if (!id) {
      return null;
    }

    // if there's a source ID only holds the profile ID
    if (!!this.profile.Source) {
      return [this.profile.Source, id];
    }

    // otherwise, ID likely contains the source
    const [source, ...rest] = id.split('/');
    if (rest.length > 0) {
      return [source, rest.join('/')];
    }

    // id does not contain a forward-slash so we
    // assume the source is local
    return ['local', id];
  }

  /**
   * Tries to get the application icon form the system.
   * Requires the app to be running in the electron wrapper.
   */
  private tryGetSystemIcon() {
    const sourceAndId = this.getIDAndSource();
    if (sourceAndId === null) {
      return;
    }

    this.sub.unsubscribe();

    this.sub = this.profileService
      .watchAppProfile(sourceAndId[0], sourceAndId[1])
      .pipe(
        switchMap((profile: AppProfile) => {
          this.updateLetter(profile);

          if (!!profile.Icons?.length) {
            const firstIcon = profile.Icons[0];

            console.log(`profile ${profile.Name} has icon of from source ${firstIcon.Source} stored in ${firstIcon.Type}`)

            switch (firstIcon.Type) {
              case 'database':
                return this.portapi
                  .get<Record & { iconData: string }>(firstIcon.Value)
                  .pipe(
                    map((result) => {
                      return result.iconData;
                    })
                  );

              case 'api':
                return of(`${this.httpAPI}/v1/profile/icon/${firstIcon.Value}`);

              case 'path':
                // TODO: Silently ignore for now.
                return of('');

              case '':
                // Icon is not set.
                return of('');

              default:
                console.error(`Icon type ${firstIcon.Type} not yet supported`);
            }
          }

          this.resovler.resolveIcon(profile);

          // return an empty icon here. If the resolver manages to find an icon
          // the profle will get updated and we'll run again here.
          return of('');
        })
      )
      .subscribe({
        next: (icon) => {
          if (iconBlobsToIgnore.some((i) => i === icon)) {
            icon = '';
          } else if (iconIDsToIgnore.some((i) => icon.includes(i))) {
            // TODO: This just checks if the value (blob, URL, etc.) contains
            // the SHA1 sum of the icon, which is used in the URL of api icon types.
            // This is very unlikely to have false positivies, but this could still
            // be done a lot cleaner.
            icon = '';
          }
          if (icon !== '') {
            this.src = this.sanitzier.bypassSecurityTrustUrl(icon);
            this.color = 'unset';
          } else {
            this.src = '';
            this.color =
              this.color === 'unset' ? 'var(--text-tertiary)' : this.color;
          }
          this.changeDetectorRef.detectChanges();
          this.parentCdr.markForCheck();
        },
        error: (err) => console.error(err),
      });
  }

  ngOnDestroy(): void {
    this.sub.unsubscribe();
  }
}

export const AppColors: string[] = [
  'rgba(244, 67, 54, .7)',
  'rgba(233, 30, 99, .7)',
  'rgba(156, 39, 176, .7)',
  'rgba(103, 58, 183, .7)',
  'rgba(63, 81, 181, .7)',
  'rgba(33, 150, 243, .7)',
  'rgba(3, 169, 244, .7)',
  'rgba(0, 188, 212, .7)',
  'rgba(0, 150, 136, .7)',
  'rgba(76, 175, 80, .7)',
  'rgba(139, 195, 74, .7)',
  'rgba(205, 220, 57, .7)',
  'rgba(255, 235, 59, .7)',
  'rgba(255, 193, 7, .7)',
  'rgba(255, 152, 0, .7)',
  'rgba(255, 87, 34, .7)',
  'rgba(121, 85, 72, .7)',
  'rgba(158, 158, 158, .7)',
  'rgba(96, 125, 139, .7)',
];
