import React from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import './styles/app.css';
import './styles/personal.css';
import './styles/workspace-layout.css';
import './styles/composer-files.css';
import './styles/model-settings.css';
import './styles/settings-workspace.css';

// Reserve native window controls before the first renderer frame.
if (window.piAPI?.platform) document.documentElement.dataset.platform = window.piAPI.platform;

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
