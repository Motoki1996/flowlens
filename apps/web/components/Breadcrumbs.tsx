import { TruncatedName } from "@/components/TruncatedName";

export type Crumb = { label: string; href?: string };

/** A crumb is a pointer to another screen, not the place its object is read,
 *  so it gets a share of the line and no more — an object name can be a
 *  paragraph long, and a trail that wrapped to three lines pushed the screen's
 *  own heading below the fold. */
const CRUMB_WIDTH = "max-w-[10rem] sm:max-w-[16rem]";

/**
 * Breadcrumbs carry the "link back to the parent object" half of
 * docs/ui-design.md rule 3. The last crumb is the current screen and is not a
 * link; every ancestor is, so a single view always reaches its collection and
 * a collection always reaches its project.
 */
export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb" className="mb-4">
      <ol className="text-muted-foreground flex flex-wrap items-center gap-1.5 text-sm">
        {items.map((item, index) => (
          <li key={`${item.label}-${index}`} className="flex items-center gap-1.5">
            {index > 0 ? <span aria-hidden>/</span> : null}
            {item.href ? (
              <TruncatedName
                href={item.href}
                text={item.label}
                className={`hover:text-foreground hover:underline ${CRUMB_WIDTH}`}
              />
            ) : (
              // The last crumb names the screen you are already on, whose
              // heading states the same name in full a few lines below — so it
              // clips without a tooltip of its own, and keeps aria-current.
              <span className={`text-foreground truncate ${CRUMB_WIDTH}`} aria-current="page">
                {item.label}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}
