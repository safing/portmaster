export interface PageSections {
  title?: string;
  choices: SupportType[];
  style?: 'small';
}

export interface QuestionSection {
  title: string;
  help?: string;
}

export interface SupportPage {
  type?: undefined;
  id: string;
  title: string;
  shortHelp: string;
  repoHelp?: string;
  prologue?: string;
  epilogue?: string;
  sections: QuestionSection[];
  privateTicket?: boolean;
  ghIssuePreset?: string;
  includeDebugData?: boolean;
  repositories?: { repo: string, name: string }[];
}

export interface ExternalLink {
  type: 'link',
  url: string;
  title: string;
  shortHelp: string;
}

export type SupportType = SupportPage | ExternalLink;

export const supportTypes: PageSections[] = [
  {
    title: "Resources",
    choices: [
      {
        type: 'link',
        title: '📘 Portmaster Wiki & FAQ',
        url: 'https://wiki.safing.io/?source=Portmaster',
        shortHelp: 'Search the Portmaster knowledge base and FAQ.',
      },
      {
        type: 'link',
        title: '🔖 Settings Handbook',
        url: 'https://docs.safing.io/portmaster/settings?source=Portmaster',
        shortHelp: 'A reference document of all Portmaster settings.'
      },
      {
        type: 'link',
        title: '📑 Safing Blog',
        url: 'https://safing.io/blog?source=Portmaster',
        shortHelp: 'Read our blog posts and announcements.',
      }
    ]
  },
  {
    title: "Communities & Support",
    style: 'small',
    choices: [
      {
        type: 'link',
        title: 'Join us on Discord',
        url: 'https://discord.gg/safing',
        shortHelp: 'Get help from the community and our AI bot on Discord.'
      },
      {
        type: 'link',
        title: 'Follow us on Mastodon',
        url: 'https://fosstodon.org/@safing',
        shortHelp: 'Get updates and privacy jokes on Mastodon.'
      },
      {
        type: 'link',
        title: 'Follow us on Twitter',
        url: 'https://twitter.com/SafingIO',
        shortHelp: 'Get updates and privacy jokes on Twitter.'
      },
      {
        type: 'link',
        title: 'Safing Support via Email',
        url: 'mailto:support@safing.io',
        shortHelp: 'As a subscriber, reach out to the Safing team directly.'
      }
    ]
  },
  {
    title: "Make a Report",
    style: 'small',
    choices: [
      {
        id: "report-bug",
        title: "🐞 Report a Bug",
        shortHelp: "Found a bug? Report your discovery and make the Portmaster better for everyone.",
        repoHelp: "Where did the bug take place?",
        sections: [
          {
            title: "What happened?",
            help: "Describe what happened in detail"
          },
          {
            title: "What did you expect to happen?",
            help: "Describe what you expected to happen instead"
          },
          {
            title: "How did you reproduce it?",
            help: "Describe how to reproduce the issue"
          },
          {
            title: "Additional information",
            help: "Provide extra details if needed"
          },
        ],
        includeDebugData: true,
        privateTicket: true,
        ghIssuePreset: "report-bug.md",
        repositories: []
      },
      {
        id: "give-feedback",
        title: "💡 Suggest an Improvement",
        shortHelp: "Suggest an enhancement or a new feature for Portmaster.",
        repoHelp: "What would you would like to improve?",
        sections: [
          {
            title: "What would you like to add or change?",
          },
          {
            title: "Why do you and others need this?"
          }
        ],
        includeDebugData: false,
        privateTicket: true,
        ghIssuePreset: "suggest-feature.md",
        repositories: []
      },
      {
        id: "compatibility-report",
        title: "📝 Make a Compatibility Report",
        shortHelp: "Report Portmaster in/compatibility with Linux Distros, VPN Clients or general Software.",
        sections: [
          {
            title: "What worked?",
            help: "Describe what worked"
          },
          {
            title: "What did not work?",
            help: "Describe what did not work in detail"
          },
          {
            title: "Additional information",
            help: "Provide extra details if needed"
          },
        ],
        includeDebugData: true,
        privateTicket: true,
        ghIssuePreset: "report-compatibility.md",
        repositories: [] // not needed with the default being "portmaster"
      },
    ],
  }
]
