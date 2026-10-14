import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "../lib";
import { splitMentions } from "../quickadd";

// Mentions travel through the markdown as links to "mention:" so they can be drawn as chips.
function withMentions(text: string, names: string[]) {
  if (!names.length) return text;
  return splitMentions(text, names).map((p) => (p.mention ? `[${p.text}](mention:)` : p.text)).join("");
}

// Markdown renders task bodies and comments: GitHub-flavoured (lists, tables, task lists, links),
// with @mentions of members shown as chips. Raw HTML is not rendered.
export function Markdown({ text, mentions = [], className }: { text: string; mentions?: string[]; className?: string }) {
  return (
    <div className={cn("md", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        urlTransform={(url) => (url === "mention:" ? url : defaultUrlTransform(url))}
        components={{
          a: ({ href, children }) =>
            href === "mention:" ? (
              <span className="rounded bg-accent px-0.5 font-medium text-foreground">{children}</span>
            ) : (
              <a href={href} target="_blank" rel="noreferrer noopener" onClick={(e) => e.stopPropagation()}>{children}</a>
            ),
        }}
      >
        {withMentions(text, mentions)}
      </ReactMarkdown>
    </div>
  );
}
