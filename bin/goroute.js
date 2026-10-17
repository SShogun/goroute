#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const { spawn } = require('child_process');

const binaryName = process.platform === 'win32' ? 'goroute.exe' : 'goroute';
const localBinPath = path.join(__dirname, binaryName);

// Check if native binary exists in npm package directory
let targetBin = localBinPath;

if (!fs.existsSync(targetBin)) {
  // Check user home bin
  const homeBin = path.join(process.env.HOME || process.env.USERPROFILE || '', '.local', 'bin', binaryName);
  if (fs.existsSync(homeBin)) {
    targetBin = homeBin;
  } else {
    // Run installer on demand
    try {
      require('../scripts/install.js');
    } catch (e) {
      console.error(`GoRoute binary not found. Please install via:`);
      console.error(`  curl -fsSL https://raw.githubusercontent.com/ziuus/goroute/main/install.sh | bash`);
      process.exit(1);
    }
  }
}

const child = spawn(targetBin, process.argv.slice(2), {
  stdio: 'inherit',
  env: process.env,
});

child.on('error', (err) => {
  console.error(`Failed to execute goroute binary: ${err.message}`);
  process.exit(1);
});

child.on('close', (code) => {
  process.exit(code ?? 0);
});
