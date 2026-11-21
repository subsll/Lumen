import AsciiOrb from "./AsciiOrb";

export default function Home() {
  return (
    <main className="page">
      <div className="brand">Lumen Forensics</div>

      <div className="orb">
        <AsciiOrb />
      </div>

      <section className="hero">
        <h1>
          Hidden in
          <br />
          Plain Sight
        </h1>
        <p>
          Lumen collects, correlates and preserves open-source intelligence—turning scattered public
          data into clear, verifiable evidence.
        </p>
        <div className="actions">
          <a className="btn-primary" href="https://github.com/subsll/lumen">Get Started</a>
          <a className="btn-ghost" href="https://github.com/subsll/lumen/README.md">See Demo</a>
        </div>
      </section>
    </main>
  );
}
