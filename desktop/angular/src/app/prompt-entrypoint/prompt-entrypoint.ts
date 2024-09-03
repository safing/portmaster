import { CommonModule } from "@angular/common";
import { Component, OnInit, TrackByFunction, inject } from "@angular/core";
import { AppProfile, AppProfileService, PortapiService } from "@safing/portmaster-api";
import { combineLatest, forkJoin, map, of, switchMap } from "rxjs";
import { ConnectionPrompt, NotificationType, NotificationsService } from "../services";
import { SfngAppIconModule } from "../shared/app-icon";
import { getCurrentWindow } from '@tauri-apps/api/window';
import { CountryFlagModule } from "../shared/country-flag";

interface Prompt {
  prompts: ConnectionPrompt[];
  profile: AppProfile;
}

@Component({
  standalone: true,
  selector: 'app-root',
  templateUrl: './prompt.html',
  imports: [
    CommonModule,
    SfngAppIconModule,
    CountryFlagModule
  ]
})
export class PromptEntryPointComponent implements OnInit {
  private readonly notificationService = inject(NotificationsService);
  private readonly portapi = inject(PortapiService);
  private readonly profileService = inject(AppProfileService);

  prompts: Prompt[] = [];

  trackPrompt: TrackByFunction<ConnectionPrompt> = (_, p) => p.EventID;
  trackProfile: TrackByFunction<Prompt> = (_, p) => p.profile._meta!.Key;

  ngOnInit(): void {

    this.notificationService
      .new$
      .pipe(
        map(notifs => {
          return notifs.filter(n => n.Type === NotificationType.Prompt && n.EventID.startsWith("filter:prompt"))
        }),
        switchMap(notifications => {
          const distictProfiles = new Map<string, ConnectionPrompt[]>();
          notifications.forEach(n => {
            const key = `${n.EventData!.Profile.Source}/${n.EventData!.Profile.ID}`
            const arr = distictProfiles.get(key) || [];
            arr.push(n);
            distictProfiles.set(key, arr);
          });

          if (distictProfiles.size === 0) {
            return of([]);
          }

          return combineLatest(Array.from(distictProfiles.entries()).map(([key, prompts]) => forkJoin({
            profile: this.profileService.getAppProfile(key),
            prompts: of(Array.from(prompts))
          })));
        })
      )
      .subscribe(result => {
        this.prompts = result;

        // show the prompt now since we're ready
        if (this.prompts.length) {
          getCurrentWindow()!.show();
        }
      })
  }

  selectAction(prompt: ConnectionPrompt, action: string) {
    prompt.SelectedActionID = action;

    this.portapi.update(prompt._meta!.Key, prompt)
      .subscribe();
  }
}
