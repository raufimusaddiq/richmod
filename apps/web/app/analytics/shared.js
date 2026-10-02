
export function SectionTitle({ id, title, description, about }) {
  return <div className="section-title"><h2 id={id} tabIndex={-1}>{title}</h2>{description && <details className="review-explainer"><summary>Tentang {about || "data ini"}</summary><p>{description}</p></details>}</div>;
}

export function Metric({ label, value }) {
  return <div><dt>{label}</dt><dd>{value}</dd></div>;
}
