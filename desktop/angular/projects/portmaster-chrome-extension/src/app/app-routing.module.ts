import { NgModule } from '@angular/core';
import { RouterModule, Routes } from '@angular/router';
import { ExtDomainListComponent } from './domain-list';
import { IntroComponent } from './welcome/intro.component';

const routes: Routes = [
  { path: '', pathMatch: 'full', component: ExtDomainListComponent },
  { path: 'authorize', pathMatch: 'prefix', component: IntroComponent }
];

@NgModule({
  imports: [RouterModule.forRoot(routes)],
  exports: [RouterModule]
})
export class AppRoutingModule { }
