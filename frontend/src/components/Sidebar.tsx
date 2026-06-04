import { NAV_GROUPS } from "../nav";

type Props = {
  active: string;
  onSelect: (key: string) => void;
};

export default function Sidebar({ active, onSelect }: Props) {
  return (
    <nav className="sidebar">
      {NAV_GROUPS.map((group) => (
        <div className="nav-group" key={group.title}>
          <div className="nav-group-title">{group.title}</div>
          {group.items.map((item) => (
            <button
              key={item.key}
              className={"nav-item" + (active === item.key ? " active" : "")}
              onClick={() => onSelect(item.key)}
            >
              {item.title}
            </button>
          ))}
        </div>
      ))}
    </nav>
  );
}
