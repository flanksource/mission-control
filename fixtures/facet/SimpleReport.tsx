import React from 'react';

interface ReportData {
  name: string;
  title: string;
  sections: Array<{
    title: string;
    content: string;
  }>;
}

export default function SimpleReport({ data }: { data: ReportData }) {
  return (
    <html>
      <head>
        <title>{data.title}</title>
        <style>{`
          body {
            font-family: system-ui, -apple-system, sans-serif;
            line-height: 1.6;
            max-width: 800px;
            margin: 0 auto;
            padding: 2rem;
            color: #333;
          }
          h1 {
            color: #2563eb;
            border-bottom: 3px solid #2563eb;
            padding-bottom: 0.5rem;
          }
          h2 {
            color: #1e40af;
            margin-top: 2rem;
          }
          section {
            margin-bottom: 2rem;
          }
        `}</style>
      </head>
      <body>
        <h1>{data.title}</h1>
        {data.sections.map((section, index) => (
          <section key={index}>
            <h2>{section.title}</h2>
            <p>{section.content}</p>
          </section>
        ))}
      </body>
    </html>
  );
}
