import { OnInit, Component, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ServiceManagerStatus, TauriIntegrationService } from 'src/app/integration/taur-app';

@Component({
  selector: 'app-root',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './app.component.html',
  styles: [
    `
      :host {
        @apply block w-screen h-screen bg-background;
      }

      #logo svg {
        @apply absolute w-20;
      }
    `,
  ],
})
export class AppComponent implements OnInit {
  private tauri = inject(TauriIntegrationService);

  status: ServiceManagerStatus | string | null = null;

  getHelp() {
    this.tauri.openExternal("https://wiki.safing.io/en/Portmaster/App")
  }

  startService() {
    this.tauri.startService()
      .then(() => this.getStatus())
      .catch(err => {
        this.status = err.error;
      });
  }

  getStatus() {
    this.tauri.getServiceManagerStatus()
      .then(result => {
        this.status = result;
      })
      .catch(err => {
        this.status = err.error;
      })
  }

  ngOnInit() {
    this.getStatus();
  }
}
