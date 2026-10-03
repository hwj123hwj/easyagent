import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import './styles/app.css';
import './styles/personal.css';
import './styles/workspace-layout.css';
import './styles/composer-files.css';
import './styles/model-settings.css';
import './styles/settings-workspace.css';

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
