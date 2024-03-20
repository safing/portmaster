import { ChangeDetectorRef, OnDestroy, Pipe, PipeTransform, inject } from "@angular/core";
import { AppProfile, AppProfileService } from "@safing/portmaster-api";
import { Subscription } from "rxjs";

@Pipe({
  name: 'toAppProfile',
  pure: false
})
export class ToAppProfilePipe implements PipeTransform, OnDestroy {
  profileService = inject(AppProfileService);
  cdr = inject(ChangeDetectorRef);

  private _lastProfile: AppProfile | null = null;
  private _lastKey: string | null = null;
  private _subscription = Subscription.EMPTY;

  transform(key: string): AppProfile | null {
    if (key !== this._lastKey) {
      this._lastKey = key;

      this._subscription.unsubscribe();
      this._subscription = this.profileService.watchAppProfile(key)
        .subscribe(value => {
          this._lastProfile = value;
          this.cdr.markForCheck();
        })
    }

    return this._lastProfile || null;
  }

  ngOnDestroy(): void {
    this._subscription.unsubscribe();
  }
}
