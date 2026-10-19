// What each of an app's tools and actions lets the assistant do, in words and an icon name, for Settings → Apps.
// Keys are Composio's tool slugs as the server's catalog chooses them (internal/core/apps.go).
export type ToolIcon =
  | "search" | "mail" | "thread" | "contacts" | "calendar" | "event" | "free" | "list"
  | "file" | "folder" | "sheet" | "message" | "channel" | "issue" | "pr" | "bell" | "repo" | "page" | "database" | "team";

const labels: Record<string, [string, ToolIcon]> = {
  GMAIL_FETCH_EMAILS: ["Search and read emails", "search"],
  GMAIL_FETCH_MESSAGE_BY_MESSAGE_ID: ["Open an email", "mail"],
  GMAIL_FETCH_MESSAGE_BY_THREAD_ID: ["Read a conversation", "thread"],
  GMAIL_LIST_THREADS: ["List conversations", "list"],
  GMAIL_SEARCH_PEOPLE: ["Look up people", "contacts"],
  GMAIL_GET_CONTACTS: ["Read contacts", "contacts"],
  GOOGLECALENDAR_EVENTS_LIST: ["List events", "calendar"],
  GOOGLECALENDAR_FIND_EVENT: ["Find an event", "search"],
  GOOGLECALENDAR_EVENTS_GET: ["Open an event", "event"],
  GOOGLECALENDAR_EVENTS_LIST_ALL_CALENDARS: ["Events across all calendars", "calendar"],
  GOOGLECALENDAR_FIND_FREE_SLOTS: ["Find free time", "free"],
  GOOGLECALENDAR_LIST_CALENDARS: ["List calendars", "list"],
  GOOGLEDRIVE_FIND_FILE: ["Find files", "search"],
  GOOGLEDRIVE_GET_DOCUMENT: ["Read documents", "file"],
  GOOGLEDRIVE_GET_FILE_METADATA: ["File details", "file"],
  GOOGLEDRIVE_EXPORT_GOOGLE_WORKSPACE_FILE: ["Read Docs, Sheets and Slides", "file"],
  GOOGLEDRIVE_READ_SPREADSHEET_VALUES: ["Read spreadsheets", "sheet"],
  GOOGLEDRIVE_FIND_FOLDER: ["Find folders", "folder"],
  SLACK_SEARCH_MESSAGES: ["Search messages", "search"],
  SLACK_FETCH_CONVERSATION_HISTORY: ["Read a channel", "message"],
  SLACK_FETCH_MESSAGE_THREAD_FROM_A_CONVERSATION: ["Read a thread", "thread"],
  SLACK_LIST_UNREAD_CHANNEL_MESSAGES: ["Unread messages", "bell"],
  SLACK_FIND_CHANNELS: ["Find channels", "channel"],
  SLACK_FIND_USERS: ["Find people", "contacts"],
  GITHUB_LIST_ISSUES_ASSIGNED_TO_THE_AUTHENTICATED_USER: ["Issues assigned to you", "issue"],
  GITHUB_SEARCH_ISSUES_AND_PULL_REQUESTS: ["Search issues and PRs", "search"],
  GITHUB_GET_AN_ISSUE: ["Open an issue", "issue"],
  GITHUB_GET_A_PULL_REQUEST: ["Open a pull request", "pr"],
  GITHUB_LIST_NOTIFICATIONS: ["Notifications", "bell"],
  GITHUB_LIST_REPOSITORIES_FOR_THE_AUTHENTICATED_USER: ["Your repositories", "repo"],
  GITHUB_GET_REPOSITORY_CONTENT: ["Read repository files", "file"],
  NOTION_SEARCH_NOTION_PAGE: ["Search pages", "search"],
  NOTION_GET_PAGE_MARKDOWN: ["Read a page", "page"],
  NOTION_RETRIEVE_PAGE: ["Page details", "page"],
  NOTION_QUERY_DATABASE: ["Query a database", "database"],
  NOTION_FETCH_DATABASE: ["Database details", "database"],
  LINEAR_SEARCH_ISSUES: ["Search issues", "search"],
  LINEAR_LIST_LINEAR_ISSUES: ["List issues", "issue"],
  LINEAR_GET_LINEAR_ISSUE: ["Open an issue", "issue"],
  LINEAR_LIST_LINEAR_PROJECTS: ["List projects", "folder"],
  LINEAR_LIST_LINEAR_TEAMS: ["List teams", "team"],
  TWITTER_USER_LOOKUP_ME: ["Your profile", "contacts"],
  TWITTER_USER_LOOKUP_BY_USERNAME: ["Look up people", "contacts"],
  TWITTER_GET_USER_MENTIONS: ["Your mentions", "bell"],
  // Actions: prepared by the assistant, sent only when you tap Send.
  GMAIL_SEND_EMAIL: ["Send emails, when you approve", "mail"],
  GMAIL_REPLY_TO_THREAD: ["Reply to emails, when you approve", "thread"],
  GMAIL_CREATE_EMAIL_DRAFT: ["Save drafts, when you approve", "mail"],
  GOOGLECALENDAR_CREATE_EVENT: ["Create events, when you approve", "event"],
  SLACK_SEND_MESSAGE: ["Send messages, when you approve", "message"],
  NOTION_CREATE_NOTION_PAGE: ["Create pages, when you approve", "page"],
  LINEAR_CREATE_LINEAR_ISSUE: ["Create issues, when you approve", "issue"],
  LINEAR_CREATE_LINEAR_COMMENT: ["Comment on issues, when you approve", "message"],
  TWITTER_CREATION_OF_A_POST: ["Post, when you approve", "message"],
  TWITTER_SEND_A_NEW_MESSAGE_TO_A_USER: ["Send direct messages, when you approve", "message"],
};

export const knownTools = Object.keys(labels);

// toolLabel is how a tool reads in the list. One the map doesn't know (added on the server since)
// still reads sensibly: its slug without the app, in sentence case.
export function toolLabel(slug: string, app: string): { text: string; icon: ToolIcon } {
  const known = labels[slug];
  if (known) return { text: known[0], icon: known[1] };
  const prefix = app.toUpperCase() + "_";
  const words = (slug.startsWith(prefix) ? slug.slice(prefix.length) : slug).toLowerCase().replace(/_/g, " ").trim();
  return { text: words.charAt(0).toUpperCase() + words.slice(1), icon: "list" };
}
