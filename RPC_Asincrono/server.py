from xmlrpc.server import SimpleXMLRPCServer
import xmlrpc.client
import time

def long_task(callback_url):
    """Simula una tarea larga y luego llama al callback del cliente."""
    print("Procesando tarea larga...")
    #PROCESAMIENTO DE LA FUNCIÓN
    time.sleep(3)  # Simula una tarea larga

    try:
        proxy = xmlrpc.client.ServerProxy(callback_url, allow_none=True)
        proxy.client_callback("Tarea completada con éxito")
        print("Callback enviado con éxito.")
        return "OK"
    except Exception as e:
        print(f"Error al llamar al callback: {e}")
    

# Crear y ejecutar el servidor RPC
server = SimpleXMLRPCServer(("localhost", 8000), allow_none=True)
print("Servidor RPC en ejecución en el puerto 8000...")
server.register_function(long_task, "long_task")

server.serve_forever()

