@echo off
cd /d F:\Nexus\OpsMesh
start /B "" "F:\Nexus\OpsMesh\opsmesh.exe" --mode=controlplane --store=memory --demo --allow-public-register=true --jwt-secret=opsmesh-demo-jwt-secret-2026 --http-port=8080 --grpc-port=9090
