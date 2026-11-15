# Portmaster

Welcome to the new Portmaster User-Interface. It's based on Angular and is built, unit and e2e tested using `@angular/cli`.

## Running locally

This section explains how to prepare your Ubuntu machine to build and test the new Portmaster User-Interface. It's recommended to use
a virtual machine but running it on bare metal will work as well. You can use the new Portmaster UI as well as the old one in parallel so
you can simply switch back when something is still missing or buggy.

1. **Prepare your tooling**

Install `git` and Node.js.

2. **Portmaster installation**

Next, make sure to install the Portmaster using the official Linux installer package.

Once the Portmaster is installed, enable **Development Mode** in its settings
(Settings → Core → Development, visible with the expertise level set to
Developer). This lifts the API restrictions so that the UI served by the
development server can talk to the Portmaster.

3. **Build and run the new UI**

Now, clone this repository and start the development server:

```bash
# Clone the repository
git clone https://github.com/safing/portmaster

# Enter the UI directory, install dependencies and start the development server
cd portmaster/desktop/angular
npm start
```

Finally open your browser and point it to http://localhost:4200.

## Hacking Quick Start

Although everything should work in the docker container as well, for the best development experience it's recommended to install `@angular/cli` locally.

It's highly recommended to:
- Use [VSCode](https://code.visualstudio.com/) (or it's oss or server-side variant) with
  - the official [Angular Language Service](https://marketplace.visualstudio.com/items?itemName=Angular.ng-template) extension
  - the [Tailwind CSS Extension Pack](https://marketplace.visualstudio.com/items?itemName=andrewmcodes.tailwindcss-extension-pack) extension
  - the [formate: CSS/LESS/SCSS formatter](https://github.com/mblander/formate) extension

### Folder Structure

From the project root (the folder containing this [README.md](./)) there are only two folders with the following content and structure:

- **`src/`** contains the actual application sources:
  - **`app/`** contains the actual application sources (components, services, uni tests ...)
    - **`layout/`** contains components that form the overall application layout. For example the navigation bar and the side dash are located there.
    - **`pages/`** contains the different pages of the application. A page is something that is associated with a dedicated application route and is rendered at the applications main content.
    - **`services/`** contains shared services (like PortAPI and friends)
    - **`shared/`** contains shared components that are likely used accross other components or pages.
    - **`widgets/`** contains widgets and their settings components for the application side dash.
    - **`debug/`** contains a debug sidebar component
  - **`assets/`** contains static assets that must be shipped seperately.
  - **`environments/`** contains build and production related environment settings (those are handled by `@angular/cli` automatically, see [angular.json](angular.json))
- **`e2e/`** contains end-to-end testing sources.


### Development server

Run `npm start` for a dev server. Navigate to `http://localhost:4200/`. The app will automatically reload if you change any of the source files.

In development mode (that is, you don't pass `--prod`) the UI expects portmaster running at `ws://127.0.0.1:817/api/database/v1`. See [environment](./src/app/environments/environment.ts).

### Code scaffolding

Run `ng generate component component-name` to generate a new component. You can also use `ng generate directive|pipe|service|class|guard|interface|enum|module`.

### Build

Run `ng build` to build the project. The build artifacts will be stored in the `dist/` directory. Use the `--prod` flag for a production build.

### Running unit tests

Run `ng test` to execute the unit tests via [Karma](https://karma-runner.github.io).

### Running end-to-end tests

Run `ng e2e` to execute the end-to-end tests via [Protractor](http://www.protractortest.org/).

### Further help

To get more help on the Angular CLI use `ng help` or go check out the [Angular CLI README](https://github.com/angular/angular-cli/blob/master/README.md).
